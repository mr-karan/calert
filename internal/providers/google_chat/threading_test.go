package google_chat

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThreadKeysAreDeterministicAndScoped(t *testing.T) {
	group := `{}:{alertname="Foo"}`
	assert.Equal(t, threadKeyForGroup("room-a", group), threadKeyForGroup("room-a", group))
	assert.NotEqual(t, threadKeyForGroup("room-a", group), threadKeyForGroup("room-b", group))
	assert.NotEqual(t, threadKeyForGroup("room-a", group), threadKeyForGroup("room-a", "other"))

	startsAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	assert.Equal(t, threadKeyForAlert("room-a", "fingerprint", startsAt), threadKeyForAlert("room-a", "fingerprint", startsAt))
	assert.NotEqual(t, threadKeyForAlert("room-a", "fingerprint", startsAt), threadKeyForAlert("room-a", "fingerprint", startsAt.Add(time.Minute)))
	assert.NotEqual(t, threadKeyForAlert("room-a", "fingerprint", startsAt), threadKeyForAlert("room-b", "fingerprint", startsAt))
}

func TestStateHashIncludesContentAndIgnoresOrder(t *testing.T) {
	alert := func(fp, summary string) alertmgrtmpl.Alert {
		return alertmgrtmpl.Alert{
			Fingerprint: fp,
			Status:      "firing",
			Labels:      alertmgrtmpl.KV{"instance": fp, "severity": "warning"},
			Annotations: alertmgrtmpl.KV{"summary": summary},
		}
	}

	first := providers.WebhookPayload{GroupKey: "group", TruncatedAlerts: 1, Data: alertmgrtmpl.Data{Status: "firing", GroupLabels: alertmgrtmpl.KV{"alertname": "test"}, CommonLabels: alertmgrtmpl.KV{"severity": "warning"}, Alerts: []alertmgrtmpl.Alert{alert("a", "first"), alert("b", "second")}}}
	reordered := first
	reordered.Alerts = []alertmgrtmpl.Alert{first.Alerts[1], first.Alerts[0]}
	assert.Equal(t, stateHash(first), stateHash(reordered))
	changed := first
	changed.Alerts = []alertmgrtmpl.Alert{alert("a", "changed"), alert("b", "second")}
	assert.NotEqual(t, stateHash(first), stateHash(changed))
	changed = first
	changed.TruncatedAlerts++
	assert.NotEqual(t, stateHash(first), stateHash(changed))
	changed = first
	changed.Status = "resolved"
	assert.NotEqual(t, stateHash(first), stateHash(changed))
	changed = first
	changed.GroupKey = "other-group"
	assert.NotEqual(t, stateHash(first), stateHash(changed))
	changed = first
	changed.CommonLabels = alertmgrtmpl.KV{"severity": "critical"}
	assert.NotEqual(t, stateHash(first), stateHash(changed))
}

func TestMemoryStoreCommitDoesNotRegressNewerState(t *testing.T) {
	store := newMemoryStore(slog.New(slog.NewJSONHandler(os.Stdout, nil)), time.Hour)
	now := time.Now()
	newer := groupState{lastHash: "new", lastPost: now.Add(time.Minute), lastStatuses: map[string]string{"a": "resolved"}}
	older := groupState{lastHash: "old", lastPost: now, lastStatuses: map[string]string{"a": "firing"}}

	require.NoError(t, store.commit(context.Background(), "room", "group", newer))
	require.NoError(t, store.commit(context.Background(), "room", "group", older))
	state, _, err := store.inspect(context.Background(), "room", "group", "new", now.Add(2*time.Minute), 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "new", state.lastHash)
	assert.Equal(t, "resolved", state.lastStatuses["a"])
}

func TestMemoryStoreExpiresStateLazilyAtTTL(t *testing.T) {
	store := newMemoryStore(slog.New(slog.NewJSONHandler(os.Stdout, nil)), time.Minute)
	now := time.Now()
	require.NoError(t, store.commit(context.Background(), "room", "group", groupState{lastHash: "hash", lastPost: now}))
	state, duplicate, err := store.inspect(context.Background(), "room", "group", "hash", now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	assert.False(t, duplicate)
	assert.Zero(t, state.lastPost)
}

func TestMemoryStoreCommitsOnlyAfterDelivery(t *testing.T) {
	store := newMemoryStore(slog.New(slog.NewJSONHandler(os.Stdout, nil)), time.Hour)
	now := time.Now()

	state, duplicate, err := store.inspect(context.Background(), "room", "group", "hash", now, time.Minute)
	require.NoError(t, err)
	assert.False(t, duplicate)
	assert.Zero(t, state.lastPost)

	// A failed render/send has no commit, so a retry remains postable.
	_, duplicate, err = store.inspect(context.Background(), "room", "group", "hash", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.False(t, duplicate)

	require.NoError(t, store.commit(context.Background(), "room", "group", groupState{lastHash: "hash", lastPost: now, lastStatuses: map[string]string{"a": "firing"}}))
	state, duplicate, err = store.inspect(context.Background(), "room", "group", "hash", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.True(t, duplicate)
	assert.Equal(t, "firing", state.lastStatuses["a"])

	_, duplicate, err = store.inspect(context.Background(), "other-room", "group", "hash", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.False(t, duplicate, "room scope must isolate group state")
}
