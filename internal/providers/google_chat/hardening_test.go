package google_chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/calert/internal/metrics"
	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupFailureDoesNotCommitDedupState(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if requests.Add(1) == 1 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			chat := newGroupModeChatStore(t, srv.URL, backend)
			chat.client.RetryMax = 0
			payload := groupPayload(groupAlert("a", "firing", "node1"))
			require.Error(t, chat.Push(context.Background(), payload), "failed delivery must be retryable")
			require.NoError(t, chat.Push(context.Background(), payload))
			require.NoError(t, chat.Push(context.Background(), payload))
			assert.Equal(t, int32(2), requests.Load(), "only the delivered payload may deduplicate")
		})
	}
}

type recordingStore struct {
	state groupState
}

func (s *recordingStore) inspect(context.Context, string, string, string, time.Time, time.Duration) (groupState, bool, error) {
	return s.state, false, nil
}
func (s *recordingStore) commit(_ context.Context, _ string, _ string, state groupState) error {
	s.state = state
	return nil
}

func TestGroupStateTimestampBeginsAfterDelivery(t *testing.T) {
	var deliveredAt time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		deliveredAt = time.Now()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	chat := newGroupModeChatStore(t, srv.URL, "memory")
	store := &recordingStore{}
	chat.groupStore = store

	require.NoError(t, chat.Push(context.Background(), groupPayload(groupAlert("a", "firing", "node1"))))
	assert.False(t, deliveredAt.IsZero())
	assert.True(t, !store.state.lastPost.Before(deliveredAt), "state timestamp must start after delivery")
}

type commitFailStore struct{}

func (commitFailStore) inspect(context.Context, string, string, string, time.Time, time.Duration) (groupState, bool, error) {
	return groupState{}, false, nil
}
func (commitFailStore) commit(context.Context, string, string, groupState) error {
	return errors.New("store unavailable")
}

func TestGroupStoreSaveFailureDoesNotFailDeliveredNotification(t *testing.T) {
	srv := newCapturingServer()
	defer srv.Close()
	chat := newGroupModeChatStore(t, srv.URL, "memory")
	chat.groupStore = commitFailStore{}

	require.NoError(t, chat.Push(context.Background(), groupPayload(groupAlert("a", "firing", "node1"))))
	assert.Len(t, srv.keys(), 1)
	var out bytes.Buffer
	chat.metrics.FlushMetrics(&out)
	assert.Contains(t, out.String(), "group_dedup_store_errors_total")
}

func TestGroupDryRunDoesNotSendOrCommit(t *testing.T) {
	srv := newCapturingServer()
	defer srv.Close()
	chat := newGroupModeChatStore(t, srv.URL, "memory")
	chat.dryRun = true
	payload := groupPayload(groupAlert("a", "firing", "node1"))

	require.NoError(t, chat.Push(context.Background(), payload))
	assert.Empty(t, srv.keys())
	_, duplicate, err := chat.groupStore.inspect(context.Background(), chat.room, payload.GroupKey, stateHash(payload), time.Now(), chat.dedupWindow)
	require.NoError(t, err)
	assert.False(t, duplicate)

	var out bytes.Buffer
	chat.metrics.FlushMetrics(&out)
	assert.NotContains(t, out.String(), "alerts_dispatched_total")
}

func TestTruncationOnlyGroupUpdateIsDelivered(t *testing.T) {
	srv := newCapturingServer()
	defer srv.Close()
	chat := newGroupModeChatStore(t, srv.URL, "memory")
	payload := groupPayload(groupAlert("a", "resolved", "node1"))
	payload.TruncatedAlerts = 1
	require.NoError(t, chat.groupStore.commit(context.Background(), chat.room, payload.GroupKey, groupState{lastHash: "previous", lastPost: time.Now(), lastStatuses: map[string]string{"a": "resolved"}}))

	require.NoError(t, chat.Push(context.Background(), payload))
	assert.Len(t, srv.keys(), 1)
}

func TestGroupTemplateEscapesDynamicValuesAndSurfacesTruncation(t *testing.T) {
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:  metrics.New("calert"),
		Endpoint: "http://test",
		Room:     "room",
		Template: "../../../static/message-group.tmpl",
	})
	require.NoError(t, err)

	alert := groupAlert("a", "firing", "node\"\\\nnext")
	alert.Annotations["summary"] = "quote \" newline\n slash\\"
	payload := groupPayload(alert)
	payload.TruncatedAlerts = 3
	ctx := buildGroupContext(payload, threadKeyForGroup("room", payload.GroupKey), 10, nil)
	msgs, err := chat.prepareMessage(ctx)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "PrometheusTargetMissing - Firing (1 firing / 0 resolved)\n", msgs[0].Text)
	assert.Len(t, msgs[0].CardsV2, 1)
	card, err := json.Marshal(msgs[0].CardsV2[0])
	require.NoError(t, err)
	assert.Contains(t, string(card), "3 additional alerts were truncated by Alertmanager")
}

func TestNewGoogleChatRejectsInvalidGroupSettings(t *testing.T) {
	base := GoogleChatOpts{
		Log:             slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:         metrics.New("calert"),
		Endpoint:        "http://test",
		Room:            "room",
		Template:        "../../../static/message-group.tmpl",
		ThreadingMode:   ThreadingModeGroup,
		ThreadTTL:       time.Hour,
		DedupWindow:     time.Minute,
		MaxAlertsPerMsg: 10,
	}

	badTTL := base
	badTTL.ThreadTTL = -time.Second
	_, err := NewGoogleChat(badTTL)
	assert.Error(t, err)

	badWindow := base
	badWindow.DedupWindow = -time.Second
	_, err = NewGoogleChat(badWindow)
	assert.Error(t, err)

	badCap := base
	badCap.MaxAlertsPerMsg = 0
	_, err = NewGoogleChat(badCap)
	assert.Error(t, err)
}

func TestProviderLogsDoNotExposeEndpointOrRedisPassword(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:             logger,
		Metrics:         metrics.New("calert"),
		Endpoint:        srv.URL + "/secret-webhook?token=endpoint-token",
		Room:            "room",
		Template:        "../../../static/message.tmpl",
		ThreadedReplies: true,
	})
	require.NoError(t, err)
	require.NoError(t, chat.Push(context.Background(), providers.WebhookPayload{Data: alertmgrtmpl.Data{Alerts: []alertmgrtmpl.Alert{groupAlert("a", "firing", "node")}}}))

	_, err = NewGoogleChat(GoogleChatOpts{
		Log:             logger,
		Metrics:         metrics.New("calert"),
		Endpoint:        "https://chat.example.test/another-secret",
		Room:            "room",
		Template:        "../../../static/message.tmpl",
		RedisAddress:    "redis.internal:6379",
		RedisPassword:   "redis-password",
		RedisKeyPrefix:  "private-prefix",
		ThreadingMode:   ThreadingModeAlert,
		MaxAlertsPerMsg: 10,
	})
	require.NoError(t, err)
	assert.NotContains(t, logs.String(), "secret-webhook")
	assert.NotContains(t, logs.String(), "endpoint-token")
	assert.NotContains(t, logs.String(), "redis-password")
}

func TestGroupModeSelectsBundledTemplateWithoutReplacingCustomTemplates(t *testing.T) {
	assert.Equal(t, "static/message-group.tmpl", templateForMode("static/message.tmpl", ThreadingModeGroup))
	assert.Equal(t, "custom.tmpl", templateForMode("custom.tmpl", ThreadingModeGroup))
	assert.Equal(t, "static/message.tmpl", templateForMode("static/message.tmpl", ThreadingModeAlert))
}

func TestLegacyAlertThreadKeysSurviveLifecycleAndRefires(t *testing.T) {
	srv := newCapturingServer()
	defer srv.Close()
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:             slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:         metrics.New("calert"),
		Endpoint:        srv.URL,
		Room:            "room",
		Template:        "../../../static/message.tmpl",
		ThreadedReplies: true,
	})
	require.NoError(t, err)

	firing := groupAlert("fingerprint", "firing", "node")
	resolved := firing
	resolved.Status = "resolved"
	refire := firing
	refire.StartsAt = firing.StartsAt.Add(time.Minute)

	require.NoError(t, chat.Push(context.Background(), providers.WebhookPayload{Data: alertmgrtmpl.Data{Alerts: []alertmgrtmpl.Alert{firing}}}))
	require.NoError(t, chat.Push(context.Background(), providers.WebhookPayload{Data: alertmgrtmpl.Data{Alerts: []alertmgrtmpl.Alert{resolved}}}))
	require.NoError(t, chat.Push(context.Background(), providers.WebhookPayload{Data: alertmgrtmpl.Data{Alerts: []alertmgrtmpl.Alert{refire}}}))
	keys := srv.keys()
	require.Len(t, keys, 3)
	assert.Equal(t, keys[0], keys[1])
	assert.NotEqual(t, keys[1], keys[2])
}
