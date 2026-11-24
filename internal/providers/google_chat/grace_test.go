package google_chat

import (
	"context"
	"testing"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIncidentGrace = 15 * time.Minute

func TestGroupModeIncidentGrace(t *testing.T) {
	for _, backend := range groupModeBackends() {
		t.Run(backend, func(t *testing.T) { testGroupModeIncidentGrace(t, backend) })
	}
}

func testGroupModeIncidentGrace(t *testing.T, backend string) {
	// Each step waits, then posts. Minutes count from the first post, which
	// happens at incidentNow, minute 120 after incidentStart.
	type step struct {
		wait  time.Duration
		alert alertmgrtmpl.Alert
	}
	run := func(t *testing.T, grace time.Duration, steps []step) []string {
		srv := newCapturingServer()
		defer srv.Close()
		chat, advance := newClockedChat(t, srv.URL, backend, ThreadRotationIncident)
		chat.incidentGrace = grace

		for _, s := range steps {
			advance(s.wait)
			require.NoError(t, chat.Push(context.Background(), groupPayload(s.alert)))
		}
		keys := srv.keys()
		require.Len(t, keys, len(steps))
		return keys
	}

	flapping := []step{
		{0, lifecycleAlert("a1", "firing", 120)},
		{10 * time.Minute, lifecycleAlert("a1", "resolved", 120)},
		{5 * time.Minute, lifecycleAlert("a2", "firing", 135)},
		{10 * time.Minute, lifecycleAlert("a2", "resolved", 135)},
		{35 * time.Minute, lifecycleAlert("a3", "firing", 180)},
	}

	t.Run("without grace every incident gets its own thread", func(t *testing.T) {
		keys := run(t, 0, flapping)
		assert.Equal(t, keys[0], keys[1])
		assert.NotEqual(t, keys[1], keys[2])
		assert.Equal(t, keys[2], keys[3])
		assert.NotEqual(t, keys[3], keys[4])
	})

	t.Run("an incident within grace of the last post stays in the thread", func(t *testing.T) {
		keys := run(t, testIncidentGrace, flapping)
		for i := 1; i < 4; i++ {
			assert.Equal(t, keys[0], keys[i], "post %d", i)
		}
		assert.NotEqual(t, keys[3], keys[4], "35 minutes of quiet is longer than the grace")
	})

	t.Run("every post extends the grace", func(t *testing.T) {
		var steps []step
		for i := 0; i < 6; i++ {
			started := 120 + i*20
			steps = append(steps,
				step{10 * time.Minute, lifecycleAlert("a1", "firing", started)},
				step{10 * time.Minute, lifecycleAlert("a1", "resolved", started)},
			)
		}
		keys := run(t, testIncidentGrace, steps)
		for i := 1; i < len(keys); i++ {
			assert.Equal(t, keys[0], keys[i], "post %d", i)
		}
	})

	t.Run("a new alert next to a silenced one stays in the thread within grace", func(t *testing.T) {
		keys := run(t, testIncidentGrace, []step{
			{0, lifecycleAlert("a1", "firing", 120)},
			{5 * time.Minute, lifecycleAlert("a2", "firing", 125)},
		})
		assert.Equal(t, keys[0], keys[1])
	})
}
