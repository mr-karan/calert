package google_chat

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	lo := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return newRedisStore(lo, client, "calert", 12*time.Hour), mr
}

func TestRedisStoreShouldPost(t *testing.T) {
	window := 5 * time.Minute
	now := time.Now()

	t.Run("first post for a new group: post, no previous statuses", func(t *testing.T) {
		r, _ := newTestRedisStore(t)
		prev, post, err := r.shouldPost("g1", "h1", map[string]string{"a": "firing"}, now, window)
		require.NoError(t, err)
		assert.True(t, post)
		assert.Nil(t, prev)
	})

	t.Run("identical hash within window: skip", func(t *testing.T) {
		r, _ := newTestRedisStore(t)
		_, _, err := r.shouldPost("g1", "h1", nil, now, window)
		require.NoError(t, err)
		_, post, err := r.shouldPost("g1", "h1", nil, now.Add(time.Minute), window)
		require.NoError(t, err)
		assert.False(t, post)
	})

	t.Run("identical hash after window: post", func(t *testing.T) {
		r, _ := newTestRedisStore(t)
		_, _, err := r.shouldPost("g1", "h1", nil, now, window)
		require.NoError(t, err)
		_, post, err := r.shouldPost("g1", "h1", nil, now.Add(window), window)
		require.NoError(t, err)
		assert.True(t, post)
	})

	t.Run("changed hash within window: post", func(t *testing.T) {
		r, _ := newTestRedisStore(t)
		_, _, err := r.shouldPost("g1", "h1", nil, now, window)
		require.NoError(t, err)
		_, post, err := r.shouldPost("g1", "h2", nil, now.Add(time.Second), window)
		require.NoError(t, err)
		assert.True(t, post)
	})

	t.Run("previous statuses returned on firing -> resolved transition", func(t *testing.T) {
		r, _ := newTestRedisStore(t)
		_, _, err := r.shouldPost("g1", "h1", map[string]string{"a": "firing", "b": "resolved"}, now, window)
		require.NoError(t, err)
		prev, post, err := r.shouldPost("g1", "h2", map[string]string{"a": "resolved", "b": "resolved"}, now.Add(time.Minute), window)
		require.NoError(t, err)
		assert.True(t, post)
		assert.Equal(t, map[string]string{"a": "firing", "b": "resolved"}, prev)
	})

	t.Run("delete clears state", func(t *testing.T) {
		r, _ := newTestRedisStore(t)
		_, _, err := r.shouldPost("g1", "h1", map[string]string{"a": "resolved"}, now, window)
		require.NoError(t, err)
		r.delete("g1")
		prev, post, err := r.shouldPost("g1", "h1", map[string]string{"a": "resolved"}, now.Add(time.Second), window)
		require.NoError(t, err)
		assert.True(t, post)
		assert.Nil(t, prev)
	})

	t.Run("write sets a TTL backstop", func(t *testing.T) {
		r, mr := newTestRedisStore(t)
		_, _, err := r.shouldPost("g1", "h1", nil, now, window)
		require.NoError(t, err)
		assert.Greater(t, mr.TTL(r.key("g1")), time.Duration(0))
	})
}

// TestRedisStoreRace is the regression test for the whole feature: two
// instances racing on the same group key and hash must yield exactly one
// post=true. This is why the operation is a single Lua script.
func TestRedisStoreRace(t *testing.T) {
	r, _ := newTestRedisStore(t)
	const n = 50
	var posts int32
	var wg sync.WaitGroup
	now := time.Now()
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, post, err := r.shouldPost("g1", "h1", nil, now, 5*time.Minute)
			require.NoError(t, err)
			if post {
				atomic.AddInt32(&posts, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), posts, "exactly one racer must win the post")
}

// TestGroupModeFailOpen: with an unreachable Redis endpoint, the dispatcher
// posts anyway and the fallback metric increments — a Redis outage degrades
// dedup, never drops an alert.
func TestGroupModeFailOpen(t *testing.T) {
	srv := newCapturingServer()
	defer srv.Close()

	mx := metrics.New("calert")
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:           slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:       mx,
		Endpoint:      srv.URL,
		Room:          "test",
		Template:      "../../../static/message-group.tmpl",
		ThreadingMode: ThreadingModeGroup,
		ThreadTTL:     12 * time.Hour,
		DedupWindow:   5 * time.Minute,
		RedisAddress:  "127.0.0.1:1", // nothing listening
	})
	require.NoError(t, err)

	require.NoError(t, chat.Push(groupPayload(groupAlert("a", "firing", "node1"))))
	assert.Len(t, srv.keys(), 1, "alert must be posted despite redis being down")

	var buf bytes.Buffer
	mx.FlushMetrics(&buf)
	assert.True(t, strings.Contains(buf.String(), `group_dedup_store_errors_total`), "fallback metric must increment")
}
