package google_chat

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/mr-karan/calert/internal/metrics"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var incidentStart = time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

// incidentNow is the default test clock: within the first thread_ttl period
// of every lifecycle the tests start.
var incidentNow = incidentStart.Add(2 * time.Hour)

const testThreadTTL = 12 * time.Hour

// newClockedChat builds a group-mode provider with the given thread rotation
// whose clock starts at incidentNow. The returned advance moves that clock, and for the Redis
// backend also moves Redis time, so stored state expires as it would in
// production.
func newClockedChat(t *testing.T, endpoint, backend, rotation string) (*GoogleChatManager, func(time.Duration)) {
	t.Helper()
	var mr *miniredis.Miniredis
	var redisAddr string
	if backend == "redis" {
		mr = miniredis.RunT(t)
		redisAddr = mr.Addr()
	}
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:             slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:         metrics.New("calert"),
		Endpoint:        endpoint,
		Room:            "test",
		Template:        "../../../static/message-group.tmpl",
		ThreadingMode:   ThreadingModeGroup,
		ThreadRotation:  rotation,
		ThreadTTL:       testThreadTTL,
		DedupWindow:     5 * time.Minute,
		MaxAlertsPerMsg: 10,
		RedisAddress:    redisAddr,
	})
	require.NoError(t, err)

	clock := incidentNow
	chat.now = func() time.Time { return clock }
	advance := func(d time.Duration) {
		clock = clock.Add(d)
		if mr != nil {
			mr.FastForward(d)
		}
	}
	return chat, advance
}

// lifecycleAlert returns an alert whose firing lifecycle starts the given
// number of minutes after incidentStart. Alertmanager gives a refire a new
// StartsAt, so tests express a refire as a new offset for the same fingerprint.
func lifecycleAlert(fp, status string, startedMinute int) alertmgrtmpl.Alert {
	a := groupAlert(fp, status, "node-"+fp)
	a.StartsAt = incidentStart.Add(time.Duration(startedMinute) * time.Minute)
	return a
}

func TestGroupModeIncidentThreads(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) { testGroupModeIncidentThreads(t, backend) })
	}
}

func testGroupModeIncidentThreads(t *testing.T, backend string) {
	t.Run("an incident keeps one thread until every alert resolves", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)
		push := func(alerts ...alertmgrtmpl.Alert) {
			require.NoError(t, chat.Push(context.Background(), groupPayload(alerts...)))
		}

		push(lifecycleAlert("a1", "firing", 0))
		push(lifecycleAlert("a1", "firing", 0), lifecycleAlert("a2", "firing", 10))
		push(lifecycleAlert("a1", "resolved", 0), lifecycleAlert("a2", "firing", 10))
		// a1 refires while a2 still holds the incident open.
		push(lifecycleAlert("a1", "firing", 30), lifecycleAlert("a2", "firing", 10))
		push(lifecycleAlert("a1", "resolved", 30), lifecycleAlert("a2", "resolved", 10))
		// Everything had resolved, so this refire is a new incident.
		push(lifecycleAlert("a1", "firing", 60))

		keys := srv.keys()
		require.Len(t, keys, 6)
		for i := 1; i < 5; i++ {
			assert.Equal(t, keys[0], keys[i], "notification %d belongs to the first incident", i+1)
		}
		assert.NotEqual(t, keys[0], keys[5], "a refire after a full resolve opens a new thread")
	})

	t.Run("a refire reported together with the last resolve stays in the incident", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)

		require.NoError(t, chat.Push(context.Background(), groupPayload(
			lifecycleAlert("a1", "firing", 0),
			lifecycleAlert("a2", "firing", 10),
		)))
		// Both resolved and a1 refired within one group_interval, so
		// Alertmanager never sent a payload where everything was resolved.
		require.NoError(t, chat.Push(context.Background(), groupPayload(
			lifecycleAlert("a1", "firing", 30),
			lifecycleAlert("a2", "resolved", 10),
		)))

		keys := srv.keys()
		require.Len(t, keys, 2)
		assert.Equal(t, keys[0], keys[1])
	})

	t.Run("a new member after a full resolve opens a new thread and hides old resolved members", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)

		require.NoError(t, chat.Push(context.Background(), groupPayload(lifecycleAlert("a1", "firing", 0))))
		require.NoError(t, chat.Push(context.Background(), groupPayload(lifecycleAlert("a1", "resolved", 0))))
		require.NoError(t, chat.Push(context.Background(), groupPayload(
			lifecycleAlert("a2", "firing", 30),
			lifecycleAlert("a1", "resolved", 0),
		)))

		keys := srv.keys()
		require.Len(t, keys, 3)
		assert.Equal(t, keys[0], keys[1])
		assert.NotEqual(t, keys[1], keys[2])
		assert.NotContains(t, srv.lastBody(), "node-a1")
	})

	t.Run("separate groups never share an incident thread", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)

		first := groupPayload(lifecycleAlert("a1", "firing", 0))
		second := groupPayload(lifecycleAlert("a1", "firing", 0))
		second.GroupKey = `{}/{}:{alertname="Other"}`
		require.NoError(t, chat.Push(context.Background(), first))
		require.NoError(t, chat.Push(context.Background(), second))

		keys := srv.keys()
		require.Len(t, keys, 2)
		assert.NotEqual(t, keys[0], keys[1])
	})

	t.Run("replicas without shared state agree on the thread while the opener fires", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		replicaA, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)
		replicaB, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)

		require.NoError(t, replicaA.Push(context.Background(), groupPayload(lifecycleAlert("a1", "firing", 0))))
		require.NoError(t, replicaB.Push(context.Background(), groupPayload(
			lifecycleAlert("a1", "firing", 0),
			lifecycleAlert("a2", "firing", 10),
		)))

		keys := srv.keys()
		require.Len(t, keys, 2)
		assert.Equal(t, keys[0], keys[1])
	})

	t.Run("lost state splits the thread once the opener has resolved", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)
		restarted, _ := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)

		require.NoError(t, chat.Push(context.Background(), groupPayload(
			lifecycleAlert("a1", "firing", 0),
			lifecycleAlert("a2", "firing", 10),
		)))
		require.NoError(t, restarted.Push(context.Background(), groupPayload(
			lifecycleAlert("a1", "resolved", 0),
			lifecycleAlert("a2", "firing", 10),
		)))

		keys := srv.keys()
		require.Len(t, keys, 2)
		assert.NotEqual(t, keys[0], keys[1], "known limitation: without state, the anchor moves to the oldest firing alert")
	})
}

func TestIncidentAnchorFor(t *testing.T) {
	now, ttl := incidentNow, testThreadTTL
	firstPeriod := func(a alertmgrtmpl.Alert) string { return a.Fingerprint + "@" + lifecycleOf(a) + "#0" }
	opened := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 0), lifecycleAlert("a2", "firing", 10)}
	anchor := openingAnchor(opened, now, ttl)
	open := groupState{threadAnchor: anchor, openLifecycles: openLifecyclesOf(opened)}
	closed := groupState{threadAnchor: anchor, openLifecycles: map[string]string{}}

	t.Run("anchors on the earliest firing alert", func(t *testing.T) {
		assert.Equal(t, firstPeriod(opened[0]), anchor)
		reordered := []alertmgrtmpl.Alert{opened[1], opened[0]}
		assert.Equal(t, anchor, openingAnchor(reordered, now, ttl))
	})

	t.Run("breaks a StartsAt tie by fingerprint", func(t *testing.T) {
		tied := []alertmgrtmpl.Alert{lifecycleAlert("b", "firing", 0), lifecycleAlert("a", "firing", 0)}
		assert.Equal(t, firstPeriod(tied[1]), openingAnchor(tied, now, ttl))
	})

	t.Run("ignores resolved alerts when any alert fires", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "resolved", 0), lifecycleAlert("a2", "firing", 10)}
		assert.Equal(t, firstPeriod(alerts[1]), openingAnchor(alerts, now, ttl))
	})

	t.Run("continues through any still-firing lifecycle", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 30), lifecycleAlert("a2", "firing", 10)}
		assert.Equal(t, anchor, incidentAnchorFor(open, alerts, now, ttl))
	})

	t.Run("continues through known state even when the opener is older than thread_ttl", func(t *testing.T) {
		assert.Equal(t, anchor, incidentAnchorFor(open, opened, now.Add(3*ttl), ttl))
	})

	t.Run("a refire of every member is a new incident", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 30), lifecycleAlert("a2", "firing", 40)}
		assert.NotEqual(t, anchor, incidentAnchorFor(open, alerts, now, ttl))
	})

	t.Run("a resolve-only payload stays with the last incident", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "resolved", 0)}
		assert.Equal(t, anchor, incidentAnchorFor(closed, alerts, now, ttl))
	})

	t.Run("a firing payload after a full resolve is a new incident", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 60)}
		assert.Equal(t, firstPeriod(alerts[0]), incidentAnchorFor(closed, alerts, now, ttl))
	})

	t.Run("state without an anchor opens an incident from the payload", func(t *testing.T) {
		legacy := groupState{lastHash: "hash", lastStatuses: map[string]string{"a1": "firing"}}
		assert.Equal(t, anchor, incidentAnchorFor(legacy, opened, now, ttl))
	})

	t.Run("without state, an opener older than thread_ttl gets a later period", func(t *testing.T) {
		late := incidentStart.Add(ttl + time.Minute)
		assert.Equal(t, "a1@"+lifecycleOf(opened[0])+"#1", openingAnchor(opened, late, ttl))
		assert.NotEqual(t, anchor, incidentAnchorFor(groupState{}, opened, late, ttl))
	})
}

func TestTTLPeriod(t *testing.T) {
	ttl := testThreadTTL
	assert.Equal(t, int64(0), ttlPeriod(incidentStart, incidentStart, ttl))
	assert.Equal(t, int64(0), ttlPeriod(incidentStart, incidentStart.Add(ttl-time.Nanosecond), ttl))
	assert.Equal(t, int64(1), ttlPeriod(incidentStart, incidentStart.Add(ttl), ttl))
	assert.Equal(t, int64(3), ttlPeriod(incidentStart, incidentStart.Add(3*ttl+time.Hour), ttl))
	assert.Equal(t, int64(0), ttlPeriod(incidentStart, incidentStart.Add(-time.Hour), ttl), "clock skew must not go negative")
	assert.Equal(t, int64(0), ttlPeriod(incidentStart, incidentStart.Add(time.Hour), 0))
	assert.Equal(t, ttlPeriod(time.Time{}, incidentStart, ttl)+1, ttlPeriod(time.Time{}, incidentStart.Add(ttl), ttl), "zero StartsAt must still advance across TTL periods")
}

func TestGroupModeThreadTTL(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) { testGroupModeThreadTTL(t, backend) })
	}
}

func testGroupModeThreadTTL(t *testing.T, backend string) {
	// A1 fires, A1 is silenced, A2 fires and resolves, then the silence on A1
	// ends. Alertmanager drops silenced alerts from the payload, so calert sees
	// only A2 while the silence lasts.
	silenceScenario := func(t *testing.T, silenceEndsAfter time.Duration) []string {
		srv := newCapturingServer()
		defer srv.Close()
		chat, advance := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)
		push := func(alerts ...alertmgrtmpl.Alert) {
			require.NoError(t, chat.Push(context.Background(), groupPayload(alerts...)))
		}

		push(lifecycleAlert("a1", "firing", 0))
		advance(10 * time.Minute)
		push(lifecycleAlert("a2", "firing", 10))
		advance(10 * time.Minute)
		push(lifecycleAlert("a2", "resolved", 10))
		advance(silenceEndsAfter)
		push(lifecycleAlert("a1", "firing", 0))

		keys := srv.keys()
		require.Len(t, keys, 4)
		assert.NotEqual(t, keys[0], keys[1], "the silenced A1 is not in the payload, so A2 opens its own thread")
		assert.Equal(t, keys[1], keys[2])
		return keys
	}

	t.Run("a silence shorter than thread_ttl returns the alert to its thread", func(t *testing.T) {
		keys := silenceScenario(t, time.Hour)
		assert.Equal(t, keys[0], keys[3])
	})

	t.Run("a silence longer than thread_ttl starts a new thread", func(t *testing.T) {
		keys := silenceScenario(t, testThreadTTL+time.Hour)
		assert.NotEqual(t, keys[0], keys[3], "must not revive the thread of A1")
		assert.NotEqual(t, keys[2], keys[3], "must not revive the thread of A2")
	})

	t.Run("reminders within thread_ttl keep a long incident in one thread", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, advance := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)

		for i := 0; i < 4; i++ {
			// Alertmanager re-sends an unchanged payload after repeat_interval.
			// The annotation changes so the dedup window does not hide it.
			alert := lifecycleAlert("a1", "firing", 0)
			alert.Annotations["reminder"] = time.Duration(i).String()
			require.NoError(t, chat.Push(context.Background(), groupPayload(alert)))
			advance(10 * time.Hour)
		}

		keys := srv.keys()
		require.Len(t, keys, 4)
		for i := 1; i < len(keys); i++ {
			assert.Equal(t, keys[0], keys[i], "reminder %d is 10h after the previous post, within thread_ttl", i)
		}
	})
}

func TestMemoryStoreKeepsIncidentState(t *testing.T) {
	store := newMemoryStore(slog.New(slog.NewJSONHandler(os.Stdout, nil)), time.Hour)
	assertStoreKeepsIncidentState(t, store)
}

func TestRedisStoreKeepsIncidentState(t *testing.T) {
	store, _ := newTestRedisStore(t)
	assertStoreKeepsIncidentState(t, store)
}

func assertStoreKeepsIncidentState(t *testing.T, store groupStateStore) {
	t.Helper()
	now := time.Now()
	open := map[string]string{"a2": lifecycleOf(lifecycleAlert("a2", "firing", 10))}

	require.NoError(t, store.commit(context.Background(), "room", "group", groupState{
		lastHash:       "hash",
		lastPost:       now,
		threadAnchor:   "a1@anchor",
		openLifecycles: open,
	}))
	state, _, err := store.inspect(context.Background(), "room", "group", "other", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "a1@anchor", state.threadAnchor)
	assert.Equal(t, open, state.openLifecycles)
}
