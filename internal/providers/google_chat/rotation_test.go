package google_chat

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupModeThreadRotation(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) { testGroupModeThreadRotation(t, backend) })
	}
}

func testGroupModeThreadRotation(t *testing.T, backend string) {
	// A1 fires and resolves, fires again an hour later, and is still firing
	// when Alertmanager re-sends it after a quiet period longer than thread_ttl.
	scenario := func(t *testing.T, rotation string) []string {
		srv := newCapturingServer()
		defer srv.Close()
		chat, advance := newClockedChat(t, srv.URL, backend, rotation)
		push := func(status string, startedMinute int) {
			alert := lifecycleAlert("a1", status, startedMinute)
			require.NoError(t, chat.Push(context.Background(), groupPayload(alert)))
		}

		push("firing", 0)
		advance(30 * time.Minute)
		push("resolved", 0)
		advance(time.Hour)
		push("firing", 90)
		advance(testThreadTTL + time.Hour)
		push("firing", 90)

		keys := srv.keys()
		require.Len(t, keys, 4)
		return keys
	}

	t.Run("none keeps one thread per group", func(t *testing.T) {
		keys := scenario(t, ThreadRotationNone)
		for _, key := range keys {
			assert.Equal(t, hashKey("test", groupPayload().GroupKey), key)
		}
	})

	t.Run("ttl starts a new thread only after thread_ttl without a post", func(t *testing.T) {
		keys := scenario(t, ThreadRotationTTL)
		assert.Equal(t, keys[0], keys[1])
		assert.Equal(t, keys[0], keys[2], "a refire within thread_ttl stays in the thread")
		assert.NotEqual(t, keys[2], keys[3])
	})

	t.Run("incident also starts a new thread for a refire after a full resolve", func(t *testing.T) {
		keys := scenario(t, ThreadRotationIncident)
		assert.Equal(t, keys[0], keys[1])
		assert.NotEqual(t, keys[1], keys[2])
		assert.NotEqual(t, keys[2], keys[3])
		assert.NotEqual(t, keys[0], keys[3])
	})

	t.Run("ttl keeps a long incident in one thread while posts arrive within thread_ttl", func(t *testing.T) {
		srv := newCapturingServer()
		defer srv.Close()
		chat, advance := newClockedChat(t, srv.URL, backend, ThreadRotationTTL)

		for i := 0; i < 4; i++ {
			alert := lifecycleAlert("a1", "firing", 0)
			alert.Annotations["reminder"] = time.Duration(i).String()
			require.NoError(t, chat.Push(context.Background(), groupPayload(alert)))
			advance(10 * time.Hour)
		}

		keys := srv.keys()
		require.Len(t, keys, 4)
		for i := 1; i < len(keys); i++ {
			assert.Equal(t, keys[0], keys[i])
		}
	})
}

func TestThreadRotationStoredState(t *testing.T) {
	for _, rotation := range []string{ThreadRotationNone, ThreadRotationTTL} {
		srv := newCapturingServer()
		chat, _ := newClockedChat(t, srv.URL, "memory", rotation)
		payload := groupPayload(lifecycleAlert("a1", "firing", 0))
		require.NoError(t, chat.Push(context.Background(), payload))
		srv.Close()

		state, _, err := chat.groupStore.inspect(context.Background(), chat.Room(), payload.GroupKey, "", incidentNow, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, state.openLifecycles, rotation)
		if rotation == ThreadRotationNone {
			assert.Empty(t, state.threadAnchor)
		} else {
			assert.NotEmpty(t, state.threadAnchor)
		}
	}
}
