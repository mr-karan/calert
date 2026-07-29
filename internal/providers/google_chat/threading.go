package google_chat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// threadKeyForGroup derives a deterministic Google Chat thread key from the
// Alertmanager GroupKey and a wall-clock time bucket. Both calert instances
// behind clustered Alertmanagers compute the same key for the same group
// without any shared state, so their messages converge into one thread.
// Still-firing groups roll into a new thread at each bucket boundary.
func threadKeyForGroup(groupKey string, now time.Time, bucket time.Duration) string {
	b := now.Unix() / int64(bucket.Seconds())
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", groupKey, b)))
	return hex.EncodeToString(sum[:])
}

// stateHash summarises a payload as the set of (fingerprint → status) pairs.
// It deliberately excludes timestamps and annotation values, which can differ
// between Alertmanager cluster nodes and would defeat duplicate detection.
func stateHash(alerts []alertmgrtmpl.Alert) string {
	pairs := make([]string, 0, len(alerts))
	for _, a := range alerts {
		pairs = append(pairs, a.Fingerprint+"="+a.Status)
	}
	sort.Strings(pairs)
	sum := sha256.Sum256([]byte(strings.Join(pairs, ",")))
	return hex.EncodeToString(sum[:])
}

// groupState holds the last posted notification for one alert group.
type groupState struct {
	lastHash string
	lastPost time.Time
	// lastStatuses records the fingerprint → status pairs of the last
	// posted payload, so an instance already shown as resolved can be
	// omitted from subsequent messages.
	lastStatuses map[string]string
}

// statusesOf extracts the fingerprint → status pairs of a payload.
func statusesOf(alerts []alertmgrtmpl.Alert) map[string]string {
	statuses := make(map[string]string, len(alerts))
	for _, a := range alerts {
		statuses[a.Fingerprint] = a.Status
	}
	return statuses
}

// groupStateStore is the dispatch path's view of group dedup state. It has a
// memory implementation (default) and a Redis implementation (shared across
// active-active instances). The dispatcher is agnostic to which one it holds.
//
// shouldPost returns post=false only for a cluster-race duplicate, the new
// state plus the *previous* statuses otherwise, and a non-nil err when the
// store was unreachable — on err the dispatcher fails open and posts anyway.
// delete is intentionally absent: group state is never deleted on the dispatch
// path (pushGroup relies on TTL to reap state, not explicit deletes). Both
// implementations keep a delete method for tests only.
type groupStateStore interface {
	shouldPost(groupKey, hash string, statuses map[string]string, now time.Time, window time.Duration) (map[string]string, bool, error)
}

// memoryStore tracks per-group posting state, in memory only. Losing it on
// restart costs at most one duplicate message and a reset dedup window.
type memoryStore struct {
	lo *slog.Logger
	sync.Mutex
	groups map[string]groupState
}

func newMemoryStore(lo *slog.Logger) *memoryStore {
	return &memoryStore{
		lo:     lo,
		groups: make(map[string]groupState),
	}
}

// shouldPost decides whether a payload with the given state hash must be
// posted. It returns false only for a cluster-race duplicate: an identical
// hash arriving within the dedup window of the last post. When it returns
// true, the new state (hash and fingerprint → status pairs) is recorded and
// the *previous* statuses are returned, so the caller can omit instances
// already shown as resolved. The memory store never errors.
func (g *memoryStore) shouldPost(groupKey, hash string, statuses map[string]string, now time.Time, window time.Duration) (map[string]string, bool, error) {
	g.Lock()
	defer g.Unlock()

	st, ok := g.groups[groupKey]
	if ok && st.lastHash == hash && now.Sub(st.lastPost) < window {
		return nil, false, nil
	}

	g.groups[groupKey] = groupState{lastHash: hash, lastPost: now, lastStatuses: statuses}
	return st.lastStatuses, true, nil
}

// delete removes a group's state, called when all of its alerts resolved.
func (g *memoryStore) delete(groupKey string) {
	g.Lock()
	defer g.Unlock()
	delete(g.groups, groupKey)
}

// prune removes groups whose last post is older than ttl, as a backstop for
// groups that never report a fully-resolved payload.
func (g *memoryStore) prune(now time.Time, ttl time.Duration) {
	g.Lock()
	defer g.Unlock()

	for k, st := range g.groups {
		if st.lastPost.Before(now.Add(-ttl)) {
			g.lo.Debug("pruning group state", "group_key", k, "last_post", st.lastPost)
			delete(g.groups, k)
		}
	}
}

// startPruneWorker periodically prunes stale group state. Blocking; run as a
// goroutine.
func (g *memoryStore) startPruneWorker(pruneInterval, ttl time.Duration) {
	for range time.NewTicker(pruneInterval).C {
		g.prune(time.Now(), ttl)
	}
}
