package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// wrap is a middleware that wraps HTTP handlers and injects the "app" context.
func wrap(app *App, next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "app", app)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resp is used to send uniform response structure.
type resp struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// sendResponse sends a JSON envelope to the HTTP response.
func sendResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out, err := json.Marshal(resp{Status: "success", Data: data})

	if err != nil {
		sendErrorResponse(w, "Internal Server Error.", http.StatusInternalServerError, nil)
		return
	}

	w.Write(out)
}

// sendErrorResponse sends a JSON error envelope to the HTTP response.
func sendErrorResponse(w http.ResponseWriter, message string, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	resp := resp{Status: "error",
		Message: message,
		Data:    data}
	out, _ := json.Marshal(resp)

	w.Write(out)
}

// Index page.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	var (
		app = r.Context().Value("app").(*App)
	)
	app.metrics.Increment(`http_requests_total{handler="index"}`)
	sendResponse(w, "welcome to calert!")
}

// Health check.
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	var (
		app = r.Context().Value("app").(*App)
	)
	app.metrics.Increment(`http_requests_total{handler="ping"}`)
	sendResponse(w, "pong")
}

// Export prometheus metrics.
func handleMetrics(w http.ResponseWriter, r *http.Request) {
	var (
		app = r.Context().Value("app").(*App)
	)
	app.metrics.FlushMetrics(w)
}

// Handle dispatching new alerts to upstream providers.
func handleDispatchNotif(w http.ResponseWriter, r *http.Request) {
	var (
		now     = time.Now()
		app     = r.Context().Value("app").(*App)
		payload = alertmgrtmpl.Data{}
	)

	app.metrics.Increment(`http_requests_total{handler="dispatch"}`)

	// Unmarshall POST Body.
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		app.lo.Error("error decoding request body", "error", err)
		app.metrics.Increment(`http_request_errors_total{handler="dispatch"}`)
		sendErrorResponse(w, "Error decoding payload.", http.StatusBadRequest, nil)
		return
	}

	roomName := r.URL.Query().Get("room_name")
	if roomName == "" {
		roomName = payload.Receiver
	}

	app.lo.Info("dispatching new alert", "room", roomName, "count", len(payload.Alerts))

	// Dispatch a list of alerts via Notifier.
	// If there are a lot of alerts (>=10) to push, G-Chat API can be extremely slow to add messages
	// to an existing thread. So it's better to enqueue it in background.
	go func() {
		if err := app.notifier.Dispatch(payload.Alerts, roomName); err != nil {
			app.lo.Error("error dispatching alerts", "error", err)
			app.metrics.Increment(`http_request_errors_total{handler="dispatch"}`)
		}
		app.metrics.Duration(`http_request_duration_seconds{handler="dispatch"}`, now)
	}()

	sendResponse(w, "dispatched")
}
