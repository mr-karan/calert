package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/calert/internal/metrics"
	"github.com/mr-karan/calert/internal/notifier"
	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	room    string
	pushed  []alertmgrtmpl.Alert
	pushErr error
}

func (m *mockProvider) ID() string   { return "mock" }
func (m *mockProvider) Room() string { return m.room }
func (m *mockProvider) Push(alerts []alertmgrtmpl.Alert) error {
	m.pushed = alerts
	return m.pushErr
}

func newTestApp(t *testing.T, provs ...providers.Provider) *App {
	lo := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := metrics.New("calert")

	n, err := notifier.Init(notifier.Opts{
		Providers: provs,
		Log:       lo,
	})
	require.NoError(t, err)

	return &App{
		lo:       lo,
		metrics:  m,
		notifier: n,
	}
}

func withAppContext(app *App, r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), "app", app)
	return r.WithContext(ctx)
}

func TestHandleIndex(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withAppContext(app, req)
	w := httptest.NewRecorder()

	handleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response resp
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "success", response.Status)
	assert.Equal(t, "welcome to calert!", response.Data)
}

func TestHandleHealthCheck(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req = withAppContext(app, req)
	w := httptest.NewRecorder()

	handleHealthCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response resp
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "success", response.Status)
	assert.Equal(t, "pong", response.Data)
}

func TestHandleMetrics(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req = withAppContext(app, req)
	w := httptest.NewRecorder()

	handleMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "calert_start_timestamp")
	assert.Contains(t, w.Body.String(), "calert_uptime_seconds")
}

func TestHandleDispatchNotif(t *testing.T) {
	t.Run("dispatches alerts successfully", func(t *testing.T) {
		prov := &mockProvider{room: "test-room"}
		app := newTestApp(t, prov)

		payload := alertmgrtmpl.Data{
			Receiver: "test-room",
			Alerts: []alertmgrtmpl.Alert{
				{Fingerprint: "abc123", Status: "firing"},
			},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/dispatch", bytes.NewReader(body))
		req = withAppContext(app, req)
		w := httptest.NewRecorder()

		handleDispatchNotif(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response resp
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "success", response.Status)
		assert.Equal(t, "dispatched", response.Data)

		time.Sleep(50 * time.Millisecond)
		assert.Len(t, prov.pushed, 1)
	})

	t.Run("uses room_name query param over receiver", func(t *testing.T) {
		prov := &mockProvider{room: "query-room"}
		app := newTestApp(t, prov)

		payload := alertmgrtmpl.Data{
			Receiver: "wrong-room",
			Alerts:   []alertmgrtmpl.Alert{{Fingerprint: "xyz"}},
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/dispatch?room_name=query-room", bytes.NewReader(body))
		req = withAppContext(app, req)
		w := httptest.NewRecorder()

		handleDispatchNotif(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		time.Sleep(50 * time.Millisecond)
		assert.Len(t, prov.pushed, 1)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		app := newTestApp(t)

		req := httptest.NewRequest(http.MethodPost, "/dispatch", bytes.NewReader([]byte("invalid json")))
		req = withAppContext(app, req)
		w := httptest.NewRecorder()

		handleDispatchNotif(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response resp
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "error", response.Status)
		assert.Contains(t, response.Message, "Error decoding payload")
	})
}

func TestSendResponse(t *testing.T) {
	w := httptest.NewRecorder()
	sendResponse(w, "test data")

	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var response resp
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "success", response.Status)
	assert.Equal(t, "test data", response.Data)
}

func TestSendErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()
	sendErrorResponse(w, "something went wrong", http.StatusBadRequest, map[string]string{"field": "value"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var response resp
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "error", response.Status)
	assert.Equal(t, "something went wrong", response.Message)
}
