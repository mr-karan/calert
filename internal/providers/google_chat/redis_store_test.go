package google_chat

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/mr-karan/calert/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisStore(t *testing.T) (*redisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return newRedisStore(slog.New(slog.NewJSONHandler(os.Stdout, nil)), client, "calert", 12*time.Hour), mr
}

func TestRedisStoreCommitsOnlyAfterDelivery(t *testing.T) {
	store, mr := newTestRedisStore(t)
	now := time.Now()

	_, duplicate, err := store.inspect(context.Background(), "room", "group", "hash", now, time.Minute)
	require.NoError(t, err)
	assert.False(t, duplicate)
	assert.False(t, mr.Exists(store.key("room", "group")), "inspection must not write state")

	// A failed render/send has no commit, so a retry is still postable.
	_, duplicate, err = store.inspect(context.Background(), "room", "group", "hash", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.False(t, duplicate)

	require.NoError(t, store.commit(context.Background(), "room", "group", groupState{lastHash: "hash", lastPost: now, lastStatuses: map[string]string{"a": "firing"}}))
	state, duplicate, err := store.inspect(context.Background(), "room", "group", "hash", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	assert.True(t, duplicate)
	assert.Equal(t, "firing", state.lastStatuses["a"])
	assert.Greater(t, mr.TTL(store.key("room", "group")), time.Duration(0))
}

func TestRedisStoreCommitDoesNotRegressNewerState(t *testing.T) {
	store, _ := newTestRedisStore(t)
	now := time.Now()
	newer := groupState{lastHash: "new", lastPost: now.Add(200 * time.Nanosecond), lastStatuses: map[string]string{"a": "resolved"}}
	older := groupState{lastHash: "old", lastPost: now.Add(100 * time.Nanosecond), lastStatuses: map[string]string{"a": "firing"}}

	require.NoError(t, store.commit(context.Background(), "room", "group", newer))
	require.NoError(t, store.commit(context.Background(), "room", "group", older))
	state, _, err := store.inspect(context.Background(), "room", "group", "new", now.Add(time.Second), 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "new", state.lastHash)
	assert.Equal(t, "resolved", state.lastStatuses["a"])
}

func TestRedisStoreKeysAreRoomScopedAndBounded(t *testing.T) {
	store, _ := newTestRedisStore(t)
	group := `{}:{alertname="sensitive / very long group key"}`
	key := store.key("room-a", group)
	assert.NotContains(t, key, group)
	assert.NotEqual(t, key, store.key("room-b", group))
	assert.Regexp(t, `^calert:group:[a-f0-9]{64}$`, key)
}

func TestGroupModeFailOpen(t *testing.T) {
	srv := newCapturingServer()
	defer srv.Close()

	mx := metrics.New("calert")
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:             slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:         mx,
		Endpoint:        srv.URL,
		Room:            "test",
		Template:        "../../../static/message-group.tmpl",
		ThreadingMode:   ThreadingModeGroup,
		ThreadTTL:       12 * time.Hour,
		DedupWindow:     5 * time.Minute,
		MaxAlertsPerMsg: 10,
		RedisAddress:    "127.0.0.1:1",
	})
	require.NoError(t, err)

	require.NoError(t, chat.Push(context.Background(), groupPayload(groupAlert("a", "firing", "node1"))))
	assert.Len(t, srv.keys(), 1)

	var buf bytes.Buffer
	mx.FlushMetrics(&buf)
	assert.Contains(t, strings.TrimSpace(buf.String()), "group_dedup_store_errors_total")
}
