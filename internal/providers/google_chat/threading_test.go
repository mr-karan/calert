package google_chat

import (
	"log/slog"
	"os"
	"testing"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
)

func TestThreadKeyForGroup(t *testing.T) {
	var (
		ttl  = 12 * time.Hour
		base = time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	)

	t.Run("two instances compute the same key for the same payload", func(t *testing.T) {
		// Simulates the two calert instances receiving the same group a
		// few seconds apart.
		k1 := threadKeyForGroup(`{}/{}:{alertname="Foo"}`, base, ttl)
		k2 := threadKeyForGroup(`{}/{}:{alertname="Foo"}`, base.Add(30*time.Second), ttl)
		assert.Equal(t, k1, k2)
	})

	t.Run("different groups get different keys", func(t *testing.T) {
		k1 := threadKeyForGroup(`{}/{}:{alertname="Foo"}`, base, ttl)
		k2 := threadKeyForGroup(`{}/{}:{alertname="Bar"}`, base, ttl)
		assert.NotEqual(t, k1, k2)
	})

	t.Run("key rolls over at the bucket boundary", func(t *testing.T) {
		boundary := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC) // 12:00 UTC bucket edge
		k1 := threadKeyForGroup("g", boundary.Add(-time.Second), ttl)
		k2 := threadKeyForGroup("g", boundary, ttl)
		assert.NotEqual(t, k1, k2)

		// Within the same bucket, key is stable.
		k3 := threadKeyForGroup("g", boundary.Add(11*time.Hour), ttl)
		assert.Equal(t, k2, k3)
	})
}

func TestStateHash(t *testing.T) {
	alert := func(fp, status string, ts time.Time) alertmgrtmpl.Alert {
		return alertmgrtmpl.Alert{
			Fingerprint: fp,
			Status:      status,
			StartsAt:    ts,
			Annotations: alertmgrtmpl.KV{"summary": ts.String()},
		}
	}
	now := time.Now()

	t.Run("insensitive to timestamps and annotations", func(t *testing.T) {
		h1 := stateHash([]alertmgrtmpl.Alert{alert("a", "firing", now)})
		h2 := stateHash([]alertmgrtmpl.Alert{alert("a", "firing", now.Add(time.Minute))})
		assert.Equal(t, h1, h2)
	})

	t.Run("insensitive to alert ordering", func(t *testing.T) {
		h1 := stateHash([]alertmgrtmpl.Alert{alert("a", "firing", now), alert("b", "resolved", now)})
		h2 := stateHash([]alertmgrtmpl.Alert{alert("b", "resolved", now), alert("a", "firing", now)})
		assert.Equal(t, h1, h2)
	})

	t.Run("sensitive to status flips", func(t *testing.T) {
		h1 := stateHash([]alertmgrtmpl.Alert{alert("a", "firing", now)})
		h2 := stateHash([]alertmgrtmpl.Alert{alert("a", "resolved", now)})
		assert.NotEqual(t, h1, h2)
	})

	t.Run("sensitive to instance set changes", func(t *testing.T) {
		h1 := stateHash([]alertmgrtmpl.Alert{alert("a", "firing", now)})
		h2 := stateHash([]alertmgrtmpl.Alert{alert("a", "firing", now), alert("b", "firing", now)})
		assert.NotEqual(t, h1, h2)
	})
}

func TestGroupStatesDedup(t *testing.T) {
	var (
		lo     = slog.New(slog.NewJSONHandler(os.Stdout, nil))
		now    = time.Now()
		window = 5 * time.Minute
	)

	// post reduces shouldPost to its dedup decision for tests that don't
	// care about the previous statuses.
	post := func(g *memoryStore, key, hash string, ts time.Time) bool {
		_, ok, _ := g.shouldPost(key, hash, nil, ts, window)
		return ok
	}

	t.Run("suppresses identical hash within window", func(t *testing.T) {
		g := newMemoryStore(lo)
		assert.True(t, post(g, "g1", "h1", now))
		assert.False(t, post(g, "g1", "h1", now.Add(time.Minute)))
	})

	t.Run("allows identical hash after window", func(t *testing.T) {
		g := newMemoryStore(lo)
		assert.True(t, post(g, "g1", "h1", now))
		assert.True(t, post(g, "g1", "h1", now.Add(window)))
	})

	t.Run("allows different hash within window", func(t *testing.T) {
		g := newMemoryStore(lo)
		assert.True(t, post(g, "g1", "h1", now))
		assert.True(t, post(g, "g1", "h2", now.Add(time.Second)))
	})

	t.Run("groups are independent", func(t *testing.T) {
		g := newMemoryStore(lo)
		assert.True(t, post(g, "g1", "h1", now))
		assert.True(t, post(g, "g2", "h1", now))
	})

	t.Run("delete resets the dedup state", func(t *testing.T) {
		g := newMemoryStore(lo)
		assert.True(t, post(g, "g1", "h1", now))
		g.delete("g1")
		assert.True(t, post(g, "g1", "h1", now.Add(time.Second)))
	})

	t.Run("prune removes stale groups only", func(t *testing.T) {
		g := newMemoryStore(lo)
		post(g, "stale", "h1", now.Add(-13*time.Hour))
		post(g, "fresh", "h1", now)
		g.prune(now, 12*time.Hour)
		assert.False(t, post(g, "fresh", "h1", now.Add(time.Second)))
		assert.True(t, post(g, "stale", "h1", now.Add(time.Second)))
	})
}

func TestGroupStatesPreviousStatuses(t *testing.T) {
	var (
		lo     = slog.New(slog.NewJSONHandler(os.Stdout, nil))
		now    = time.Now()
		window = 5 * time.Minute
	)

	t.Run("first post has no previous statuses", func(t *testing.T) {
		g := newMemoryStore(lo)
		prev, ok, _ := g.shouldPost("g1", "h1", map[string]string{"a": "firing"}, now, window)
		assert.True(t, ok)
		assert.Nil(t, prev)
	})

	t.Run("subsequent post returns the previously recorded statuses", func(t *testing.T) {
		g := newMemoryStore(lo)
		g.shouldPost("g1", "h1", map[string]string{"a": "firing", "b": "resolved"}, now, window)
		prev, ok, _ := g.shouldPost("g1", "h2", map[string]string{"a": "resolved", "b": "resolved"}, now.Add(time.Minute), window)
		assert.True(t, ok)
		assert.Equal(t, map[string]string{"a": "firing", "b": "resolved"}, prev)
	})

	t.Run("delete forgets statuses, restart amnesia is benign", func(t *testing.T) {
		g := newMemoryStore(lo)
		g.shouldPost("g1", "h1", map[string]string{"a": "resolved"}, now, window)
		g.delete("g1")
		prev, ok, _ := g.shouldPost("g1", "h2", map[string]string{"a": "resolved"}, now.Add(time.Minute), window)
		assert.True(t, ok)
		assert.Nil(t, prev)
	})
}
