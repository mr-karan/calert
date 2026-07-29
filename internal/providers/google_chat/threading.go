package google_chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// threadKeyForGroup is stable for a room and Alertmanager group. threadTTL
// controls state retention only; it never rolls a still-active group into a
// different Google Chat thread.
func threadKeyForGroup(room, groupKey string) string {
	return hashKey(room, groupKey)
}

// threadKeyForAlert is the legacy per-alert thread key. StartsAt distinguishes
// a refire from the previous alert lifecycle while keeping firing and resolved
// notifications for the same lifecycle in one thread across restarts.
func threadKeyForAlert(room, fingerprint string, startsAt time.Time) string {
	return hashKey(room, fingerprint, startsAt.UTC().Format(time.RFC3339Nano))
}

func hashKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// stateHash captures the notification content that users can observe, apart
// from timestamps that vary between Alertmanager peers. Sorting makes it
// independent of alert, label, and annotation ordering.
func stateHash(payload providers.WebhookPayload) string {
	entries := make([]string, 0, len(payload.Alerts))
	for _, a := range payload.Alerts {
		entries = append(entries, strings.Join([]string{
			a.Fingerprint,
			a.Status,
			canonicalKV(a.Labels),
			canonicalKV(a.Annotations),
			a.GeneratorURL,
		}, "\x00"))
	}
	sort.Strings(entries)
	return hashKey(
		payload.Status,
		payload.GroupKey,
		canonicalKV(payload.GroupLabels),
		canonicalKV(payload.CommonLabels),
		fmt.Sprintf("%d", payload.TruncatedAlerts),
		strings.Join(entries, "\x01"),
	)
}

func canonicalKV(kv alertmgrtmpl.KV) string {
	keys := make([]string, 0, len(kv))
	for key := range kv {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"\x00"+kv[key])
	}
	return strings.Join(parts, "\x01")
}

// groupState holds the last successfully delivered notification for a group.
type groupState struct {
	lastHash     string
	lastPost     time.Time
	lastStatuses map[string]string
}

func statusesOf(alerts []alertmgrtmpl.Alert) map[string]string {
	statuses := make(map[string]string, len(alerts))
	for _, a := range alerts {
		statuses[a.Fingerprint] = a.Status
	}
	return statuses
}

// groupStateStore deliberately separates inspection from commit. A failed
// render or send must not suppress a retry, so state is committed only after
// every message has been delivered.
type groupStateStore interface {
	inspect(ctx context.Context, room, groupKey, hash string, now time.Time, window time.Duration) (groupState, bool, error)
	commit(ctx context.Context, room, groupKey string, state groupState) error
}

type memoryStore struct {
	lo  *slog.Logger
	ttl time.Duration
	sync.Mutex
	groups map[string]groupState
}

func newMemoryStore(lo *slog.Logger, ttl time.Duration) *memoryStore {
	return &memoryStore{lo: lo, ttl: ttl, groups: make(map[string]groupState)}
}

func (g *memoryStore) inspect(_ context.Context, room, groupKey, hash string, now time.Time, window time.Duration) (groupState, bool, error) {
	g.Lock()
	defer g.Unlock()

	key := groupStoreKey(room, groupKey)
	state := g.groups[key]
	if !state.lastPost.IsZero() && now.Sub(state.lastPost) >= g.ttl {
		delete(g.groups, key)
		state = groupState{}
	}
	if !state.lastPost.IsZero() && state.lastHash == hash && now.Sub(state.lastPost) < window {
		return state, true, nil
	}
	return state, false, nil
}

func (g *memoryStore) commit(_ context.Context, room, groupKey string, state groupState) error {
	g.Lock()
	defer g.Unlock()
	key := groupStoreKey(room, groupKey)
	if previous, ok := g.groups[key]; ok && state.lastPost.Before(previous.lastPost) {
		return nil
	}
	g.groups[key] = state
	return nil
}

func groupStoreKey(room, groupKey string) string {
	return hashKey(room, groupKey)
}
