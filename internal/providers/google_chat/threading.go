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

// threadKeyForGroup keeps the key of the "none" rotation, hash(room,
// GroupKey), for an empty anchor, so existing deployments keep their threads.
func threadKeyForGroup(room, groupKey, anchor string) string {
	if anchor == "" {
		return hashKey(room, groupKey)
	}
	return hashKey(room, groupKey, anchor)
}

// lifecycleOf identifies one firing lifecycle of an alert. Alertmanager keeps
// StartsAt for as long as the alert stays active, merging overlapping
// re-sends to the earliest value, and assigns a new StartsAt to a refire.
func lifecycleOf(a alertmgrtmpl.Alert) string {
	return a.StartsAt.UTC().Format(time.RFC3339Nano)
}

func groupAnchorFor(rotation string, previous groupState, alerts []alertmgrtmpl.Alert, now time.Time, threadTTL, incidentGrace time.Duration) string {
	switch rotation {
	case ThreadRotationTTL:
		return rotationAnchorFor(previous, alerts, now, threadTTL)
	case ThreadRotationIncident:
		if withinIncidentGrace(previous, now, incidentGrace) {
			return previous.threadAnchor
		}
		return incidentAnchorFor(previous, alerts, now, threadTTL)
	default:
		return ""
	}
}

// rotationAnchorFor keeps the thread for as long as the group has state.
// Known state is never older than threadTTL, because both stores expire it
// then, so a group starts a new thread only after threadTTL without a post.
func rotationAnchorFor(previous groupState, alerts []alertmgrtmpl.Alert, now time.Time, threadTTL time.Duration) string {
	if previous.threadAnchor != "" {
		return previous.threadAnchor
	}
	return openingAnchor(alerts, now, threadTTL)
}

// withinIncidentGrace keeps any new incident in the thread of the previous one
// while the group posted less than incidentGrace ago. Every post extends the
// grace, so alerts that keep flapping share one thread until the group stays
// quiet for incidentGrace.
func withinIncidentGrace(previous groupState, now time.Time, incidentGrace time.Duration) bool {
	return incidentGrace > 0 && previous.threadAnchor != "" && now.Sub(previous.lastPost) < incidentGrace
}

// incidentAnchorFor adds incident detection to rotationAnchorFor. A payload
// continues the incident when it carries any alert lifecycle that was firing
// in the last delivered notification, so an incident lasts until every one of
// its alerts has resolved. A payload without firing alerts only reports
// endings, so it never opens an incident while one is known.
func incidentAnchorFor(previous groupState, alerts []alertmgrtmpl.Alert, now time.Time, threadTTL time.Duration) string {
	if previous.threadAnchor == "" || !hasFiring(alerts) {
		return rotationAnchorFor(previous, alerts, now, threadTTL)
	}
	for _, a := range alerts {
		if lifecycle, ok := previous.openLifecycles[a.Fingerprint]; ok && lifecycle == lifecycleOf(a) {
			return previous.threadAnchor
		}
	}
	return openingAnchor(alerts, now, threadTTL)
}

// openingAnchor is derived from the payload and the clock only, so replicas
// without shared state agree on the thread of a fresh incident. The anchor
// carries the number of whole threadTTL periods since the opening alert
// started. Without state, such as after a silence longer than threadTTL, an
// old alert therefore lands in a later period and a new thread instead of
// reviving a thread that may have been quiet for that long.
func openingAnchor(alerts []alertmgrtmpl.Alert, now time.Time, threadTTL time.Duration) string {
	candidates := alerts
	if hasFiring(alerts) {
		candidates = firingOnly(alerts)
	}
	if len(candidates) == 0 {
		return ""
	}

	earliest := candidates[0]
	for _, a := range candidates[1:] {
		if a.StartsAt.Before(earliest.StartsAt) ||
			(a.StartsAt.Equal(earliest.StartsAt) && a.Fingerprint < earliest.Fingerprint) {
			earliest = a
		}
	}
	return fmt.Sprintf("%s@%s#%d", earliest.Fingerprint, lifecycleOf(earliest), ttlPeriod(earliest.StartsAt, now, threadTTL))
}

func ttlPeriod(startsAt, now time.Time, threadTTL time.Duration) int64 {
	if threadTTL <= 0 {
		return 0
	}
	if startsAt.IsZero() {
		if now.UnixNano() <= 0 {
			return 0
		}
		return now.UnixNano() / threadTTL.Nanoseconds()
	}
	age := now.Sub(startsAt)
	if age <= 0 {
		return 0
	}
	return int64(age / threadTTL)
}

func hasFiring(alerts []alertmgrtmpl.Alert) bool {
	for _, a := range alerts {
		if a.Status == "firing" {
			return true
		}
	}
	return false
}

func firingOnly(alerts []alertmgrtmpl.Alert) []alertmgrtmpl.Alert {
	firing := make([]alertmgrtmpl.Alert, 0, len(alerts))
	for _, a := range alerts {
		if a.Status == "firing" {
			firing = append(firing, a)
		}
	}
	return firing
}

func openLifecyclesOf(alerts []alertmgrtmpl.Alert) map[string]string {
	open := make(map[string]string)
	for _, a := range alerts {
		if a.Status == "firing" {
			open[a.Fingerprint] = lifecycleOf(a)
		}
	}
	return open
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
	// threadAnchor is the anchor of the thread the last notification was
	// posted to. Empty with the "none" rotation and when no state is known.
	threadAnchor string
	// openLifecycles maps fingerprint to lifecycle for the alerts that were
	// firing in the last notification. Kept only by the "incident" rotation.
	openLifecycles map[string]string
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
