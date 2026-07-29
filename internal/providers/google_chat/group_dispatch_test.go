package google_chat

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/mr-karan/calert/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturingServer struct {
	*httptest.Server
	mu         sync.Mutex
	threadKeys []string
	bodies     []string
}

func newCapturingServer() *capturingServer {
	cs := &capturingServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cs.mu.Lock()
		cs.threadKeys = append(cs.threadKeys, r.URL.Query().Get("threadKey"))
		cs.bodies = append(cs.bodies, string(body))
		cs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	return cs
}

func (cs *capturingServer) keys() []string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return append([]string(nil), cs.threadKeys...)
}

func (cs *capturingServer) lastBody() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.bodies) == 0 {
		return ""
	}
	return cs.bodies[len(cs.bodies)-1]
}

func newGroupModeChat(t *testing.T, endpoint string) *GoogleChatManager {
	t.Helper()
	return newGroupModeChatStore(t, endpoint, "")
}

// newGroupModeChatStore builds a group-mode provider backed by the memory
// store (backend == "memory") or a Redis store on a fresh per-call miniredis
// (backend == "redis"). A fresh Redis per call keeps subtests isolated, just
// like each memory-backed chat gets its own map. The whole dispatch suite runs
// against both so they are proven observationally equivalent.
func newGroupModeChatStore(t *testing.T, endpoint, backend string) *GoogleChatManager {
	t.Helper()
	var redisAddr string
	if backend == "redis" {
		redisAddr = miniredis.RunT(t).Addr()
	}
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:           slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:       metrics.New("calert"),
		Endpoint:      endpoint,
		Room:          "test",
		Template:      "../../../static/message-group.tmpl",
		ThreadingMode: ThreadingModeGroup,
		ThreadTTL:     12 * time.Hour,
		DedupWindow:   5 * time.Minute,
		RedisAddress:  redisAddr,
	})
	require.NoError(t, err)
	return chat
}

// groupModeBackends names the store backends the dispatch suite runs against.
func groupModeBackends() []string {
	return []string{"memory", "redis"}
}

func TestGroupModeDispatch(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) { testGroupModeDispatch(t, backend) })
	}
}

func testGroupModeDispatch(t *testing.T, backend string) {
	t.Run("posts one message per webhook into the deterministic thread", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat := newGroupModeChatStore(t, srv.URL, backend)

		payload := groupPayload(
			groupAlert("a", "firing", "node1"),
			groupAlert("b", "firing", "node2"),
		)

		require.NoError(t, chat.Push(payload))

		keys := srv.keys()
		require.Len(t, keys, 1, "one webhook payload must produce exactly one message")
		expected := threadKeyForGroup(payload.GroupKey, time.Now(), 12*time.Hour)
		assert.Equal(t, expected, keys[0])
	})

	t.Run("suppresses identical payload within the dedup window", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat := newGroupModeChatStore(t, srv.URL, backend)

		payload := groupPayload(groupAlert("a", "firing", "node1"))

		require.NoError(t, chat.Push(payload))
		require.NoError(t, chat.Push(payload))
		assert.Len(t, srv.keys(), 1, "duplicate payload within window must be dropped")
	})

	t.Run("posts again on state change within the window", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat := newGroupModeChatStore(t, srv.URL, backend)

		require.NoError(t, chat.Push(groupPayload(groupAlert("a", "firing", "node1"))))
		require.NoError(t, chat.Push(groupPayload(
			groupAlert("a", "firing", "node1"),
			groupAlert("b", "firing", "node2"),
		)))

		keys := srv.keys()
		require.Len(t, keys, 2)
		assert.Equal(t, keys[0], keys[1], "updates stay in the same thread")
	})

	t.Run("identical resolved re-send within window is deduped, but a refire posts", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat := newGroupModeChatStore(t, srv.URL, backend)

		firing := groupPayload(groupAlert("a", "firing", "node1"))
		resolved := groupPayload(groupAlert("a", "resolved", "node1"))

		require.NoError(t, chat.Push(firing))    // key 1
		require.NoError(t, chat.Push(resolved))  // key 2: firing→resolved transition
		// State is kept after full resolve (TTL forgets it later), so an
		// identical resolved re-send within the window — e.g. from the second
		// Alertmanager — is suppressed instead of posting a duplicate card.
		require.NoError(t, chat.Push(resolved))
		assert.Len(t, srv.keys(), 2)

		// A genuine refire is a new state hash, so it posts.
		require.NoError(t, chat.Push(firing))
		assert.Len(t, srv.keys(), 3)
	})

	t.Run("resolved instances do not reappear when a new member fires", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat := newGroupModeChatStore(t, srv.URL, backend)

		// Two instances fire, then both resolve (fully resolved card posted).
		require.NoError(t, chat.Push(groupPayload(
			groupAlert("a", "firing", "node1"),
			groupAlert("b", "firing", "node2"),
		)))
		require.NoError(t, chat.Push(groupPayload(
			groupAlert("a", "resolved", "node1"),
			groupAlert("b", "resolved", "node2"),
		)))

		// A new member fires; Alertmanager re-sends the lingering resolved
		// members in the same group. They were already shown resolved, so the
		// body must show only the new firing instance — not the old resolved
		// ones reappearing.
		require.NoError(t, chat.Push(groupPayload(
			groupAlert("c", "firing", "node3"),
			groupAlert("a", "resolved", "node1"),
			groupAlert("b", "resolved", "node2"),
		)))
		body := srv.lastBody()
		assert.Contains(t, body, "node3")
		assert.NotContains(t, body, "node1")
		assert.NotContains(t, body, "node2")
	})
}

func TestGroupModeShowsResolvedOnlyOnce(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) { testGroupModeShowsResolvedOnlyOnce(t, backend) })
	}
}

func testGroupModeShowsResolvedOnlyOnce(t *testing.T, backend string) {
	srv := newCapturingServer()
	defer srv.Close()
	chat := newGroupModeChatStore(t, srv.URL, backend)

	// node2 resolves: its transition is rendered.
	require.NoError(t, chat.Push(groupPayload(
		groupAlert("a", "firing", "node1"),
		groupAlert("b", "firing", "node2"),
	)))
	require.NoError(t, chat.Push(groupPayload(
		groupAlert("a", "firing", "node1"),
		groupAlert("b", "resolved", "node2"),
	)))
	assert.Contains(t, srv.lastBody(), "node2")

	// node1 resolves next: node2 was already shown as resolved and must
	// not reappear, and the header counts only the rendered node1.
	require.NoError(t, chat.Push(groupPayload(
		groupAlert("a", "resolved", "node1"),
		groupAlert("b", "resolved", "node2"),
	)))
	body := srv.lastBody()
	assert.Contains(t, body, "node1")
	assert.NotContains(t, body, "node2")
	assert.Contains(t, body, "0 firing / 1 resolved")
	assert.Len(t, srv.keys(), 3)
}
