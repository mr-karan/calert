package notifier

import (
	"log/slog"
	"os"
	"testing"

	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements providers.Provider for testing
type mockProvider struct {
	id      string
	room    string
	pushed  []alertmgrtmpl.Alert
	pushErr error
}

func (m *mockProvider) ID() string {
	return m.id
}

func (m *mockProvider) Room() string {
	return m.room
}

func (m *mockProvider) Push(alerts []alertmgrtmpl.Alert) error {
	m.pushed = alerts
	return m.pushErr
}

func TestInit(t *testing.T) {
	lo := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	t.Run("initializes with providers", func(t *testing.T) {
		prov1 := &mockProvider{id: "google_chat", room: "room1"}
		prov2 := &mockProvider{id: "google_chat", room: "room2"}

		notif, err := Init(Opts{
			Providers: []providers.Provider{prov1, prov2},
			Log:       lo,
		})

		require.NoError(t, err)
		assert.Len(t, notif.providers, 2)
	})

	t.Run("initializes with empty providers", func(t *testing.T) {
		notif, err := Init(Opts{
			Providers: []providers.Provider{},
			Log:       lo,
		})

		require.NoError(t, err)
		assert.Len(t, notif.providers, 0)
	})

	t.Run("overwrites duplicate rooms", func(t *testing.T) {
		prov1 := &mockProvider{id: "provider1", room: "same-room"}
		prov2 := &mockProvider{id: "provider2", room: "same-room"}

		notif, err := Init(Opts{
			Providers: []providers.Provider{prov1, prov2},
			Log:       lo,
		})

		require.NoError(t, err)
		// Last provider wins
		assert.Len(t, notif.providers, 1)
		assert.Equal(t, "provider2", notif.providers["same-room"].ID())
	})
}

func TestDispatch(t *testing.T) {
	lo := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	t.Run("dispatches alerts to correct room", func(t *testing.T) {
		prov := &mockProvider{id: "google_chat", room: "test-room"}

		notif, err := Init(Opts{
			Providers: []providers.Provider{prov},
			Log:       lo,
		})
		require.NoError(t, err)

		alerts := []alertmgrtmpl.Alert{
			{Fingerprint: "alert1"},
			{Fingerprint: "alert2"},
		}

		err = notif.Dispatch(alerts, "test-room")
		require.NoError(t, err)

		assert.Len(t, prov.pushed, 2)
		assert.Equal(t, "alert1", prov.pushed[0].Fingerprint)
		assert.Equal(t, "alert2", prov.pushed[1].Fingerprint)
	})

	t.Run("returns error for unknown room with available rooms", func(t *testing.T) {
		prov := &mockProvider{id: "google_chat", room: "test-room"}

		notif, err := Init(Opts{
			Providers: []providers.Provider{prov},
			Log:       lo,
		})
		require.NoError(t, err)

		err = notif.Dispatch([]alertmgrtmpl.Alert{}, "unknown-room")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no provider configured for room: unknown-room")
		assert.Contains(t, err.Error(), "test-room")
	})

	t.Run("returns hint for kubernetes namespaced room", func(t *testing.T) {
		prov := &mockProvider{id: "google_chat", room: "alertas"}

		notif, err := Init(Opts{
			Providers: []providers.Provider{prov},
			Log:       lo,
		})
		require.NoError(t, err)

		err = notif.Dispatch([]alertmgrtmpl.Alert{}, "cattle-monitoring-system/alertas/alertas")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "room_name=")
		assert.Contains(t, err.Error(), "Kubernetes")
	})

	t.Run("dispatches to multiple rooms", func(t *testing.T) {
		prov1 := &mockProvider{id: "google_chat", room: "room1"}
		prov2 := &mockProvider{id: "google_chat", room: "room2"}

		notif, err := Init(Opts{
			Providers: []providers.Provider{prov1, prov2},
			Log:       lo,
		})
		require.NoError(t, err)

		alertsRoom1 := []alertmgrtmpl.Alert{{Fingerprint: "alert-room1"}}
		alertsRoom2 := []alertmgrtmpl.Alert{{Fingerprint: "alert-room2"}}

		err = notif.Dispatch(alertsRoom1, "room1")
		require.NoError(t, err)

		err = notif.Dispatch(alertsRoom2, "room2")
		require.NoError(t, err)

		assert.Equal(t, "alert-room1", prov1.pushed[0].Fingerprint)
		assert.Equal(t, "alert-room2", prov2.pushed[0].Fingerprint)
	})
}
