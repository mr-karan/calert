package google_chat

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var incidentStart = time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

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
		chat := newGroupModeChatStore(t, srv.URL, backend)
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
		chat := newGroupModeChatStore(t, srv.URL, backend)

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
		chat := newGroupModeChatStore(t, srv.URL, backend)

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
		chat := newGroupModeChatStore(t, srv.URL, backend)

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
		replicaA := newGroupModeChatStore(t, srv.URL, backend)
		replicaB := newGroupModeChatStore(t, srv.URL, backend)

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
		chat := newGroupModeChatStore(t, srv.URL, backend)
		restarted := newGroupModeChatStore(t, srv.URL, backend)

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
	opened := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 0), lifecycleAlert("a2", "firing", 10)}
	anchor := openingAnchor(opened)
	open := groupState{incidentAnchor: anchor, openLifecycles: openLifecyclesOf(opened)}
	closed := groupState{incidentAnchor: anchor, openLifecycles: map[string]string{}}

	t.Run("anchors on the earliest firing alert", func(t *testing.T) {
		assert.Equal(t, "a1@"+lifecycleOf(opened[0]), anchor)
		reordered := []alertmgrtmpl.Alert{opened[1], opened[0]}
		assert.Equal(t, anchor, openingAnchor(reordered))
	})

	t.Run("breaks a StartsAt tie by fingerprint", func(t *testing.T) {
		tied := []alertmgrtmpl.Alert{lifecycleAlert("b", "firing", 0), lifecycleAlert("a", "firing", 0)}
		assert.Equal(t, "a@"+lifecycleOf(tied[1]), openingAnchor(tied))
	})

	t.Run("ignores resolved alerts when any alert fires", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "resolved", 0), lifecycleAlert("a2", "firing", 10)}
		assert.Equal(t, "a2@"+lifecycleOf(alerts[1]), openingAnchor(alerts))
	})

	t.Run("continues through any still-firing lifecycle", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 30), lifecycleAlert("a2", "firing", 10)}
		assert.Equal(t, anchor, incidentAnchorFor(open, alerts))
	})

	t.Run("a refire of every member is a new incident", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 30), lifecycleAlert("a2", "firing", 40)}
		assert.NotEqual(t, anchor, incidentAnchorFor(open, alerts))
	})

	t.Run("a resolve-only payload stays with the last incident", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "resolved", 0)}
		assert.Equal(t, anchor, incidentAnchorFor(closed, alerts))
	})

	t.Run("a firing payload after a full resolve is a new incident", func(t *testing.T) {
		alerts := []alertmgrtmpl.Alert{lifecycleAlert("a1", "firing", 60)}
		assert.Equal(t, "a1@"+lifecycleOf(alerts[0]), incidentAnchorFor(closed, alerts))
	})

	t.Run("state without an anchor opens an incident from the payload", func(t *testing.T) {
		legacy := groupState{lastHash: "hash", lastStatuses: map[string]string{"a1": "firing"}}
		assert.Equal(t, anchor, incidentAnchorFor(legacy, opened))
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
		incidentAnchor: "a1@anchor",
		openLifecycles: open,
	}))
	state, _, err := store.inspect(context.Background(), "room", "group", "other", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "a1@anchor", state.incidentAnchor)
	assert.Equal(t, open, state.openLifecycles)
}
