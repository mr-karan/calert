package google_chat

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/mr-karan/calert/internal/metrics"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type GoogleChatManager struct {
	lo              *slog.Logger
	metrics         *metrics.Manager
	activeAlerts    *ActiveAlerts
	endpoint        string
	room            string
	client          *http.Client
	msgTmpl         *template.Template
	dryRun          bool
	threadedReplies bool
}

type GoogleChatOpts struct {
	Log             *slog.Logger
	Metrics         *metrics.Manager
	DryRun          bool
	MaxIdleConn     int
	Timeout         time.Duration
	ProxyURL        string
	Endpoint        string
	Room            string
	Template        string
	ThreadTTL       time.Duration
	ThreadedReplies bool
}

// NewGoogleChat initializes a Google Chat provider object.
func NewGoogleChat(opts GoogleChatOpts) (*GoogleChatManager, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost: opts.MaxIdleConn,
	}

	// Add a proxy to make upstream requests if specified in config.
	if opts.ProxyURL != "" {
		u, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing proxy URL: %s", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}

	// Initialise a generic HTTP Client for communicating with the G-Chat APIs.
	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
	}

	// Initialise the map of active alerts.
	alerts := make(map[string]AlertDetails, 0)

	// Initialise message template functions.
	templateFuncMap := template.FuncMap{
		"Title": func(s string) string {
			// Create a Title cased string respecting Unicode rules.
			titleCaser := cases.Title(language.English)
			return titleCaser.String(s)
		},
		"toUpper":  strings.ToUpper,
		"Contains": strings.Contains,
	}

	// Load the template.
	tmpl, err := template.New(filepath.Base(opts.Template)).Funcs(templateFuncMap).ParseFiles(opts.Template)
	if err != nil {
		return nil, err
	}

	mgr := &GoogleChatManager{
		lo:       opts.Log,
		metrics:  opts.Metrics,
		client:   client,
		endpoint: opts.Endpoint,
		room:     opts.Room,
		activeAlerts: &ActiveAlerts{
			alerts:  alerts,
			lo:      opts.Log,
			metrics: opts.Metrics,
		},
		msgTmpl:         tmpl,
		dryRun:          opts.DryRun,
		threadedReplies: opts.ThreadedReplies,
	}
	// Start a background worker to cleanup alerts based on TTL mechanism.
	go mgr.activeAlerts.startPruneWorker(1*time.Hour, opts.ThreadTTL)

	return mgr, nil
}

// Push accepts the list of alerts and dispatches them to Webhook API endpoint.
func (m *GoogleChatManager) Push(alerts []alertmgrtmpl.Alert) error {
	m.lo.Info("dispatching alerts to google chat", "count", len(alerts))

	// For each alert, lookup the UUID and send the alert.
	for _, a := range alerts {
		// If it's a new alert whose fingerprint isn't in the active alerts map, add it first.
		if m.activeAlerts.loookup(a.Fingerprint) == "" {
			m.activeAlerts.add(a)
		}

		// Prepare a list of messages to send.
		msgs, err := m.prepareMessage(a)
		if err != nil {
			m.lo.Error("error preparing message", "error", err)
			continue
		}

		// Dispatch an HTTP request for each message.
		for _, msg := range msgs {
			var (
				threadKey = m.activeAlerts.alerts[a.Fingerprint].UUID.String()
				now       = time.Now()
			)

			m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_total{provider="%s", room="%s"}`, m.ID(), m.Room()))

			// Send message to API.
			if m.dryRun {
				m.lo.Info("dry_run is enabled for this room. skipping pushing notification", "room", m.Room())
			} else {
				if err := m.sendMessage(msg, threadKey); err != nil {
					m.metrics.Increment(fmt.Sprintf(`alerts_dispatched_errors_total{provider="%s", room="%s"}`, m.ID(), m.Room()))
					m.lo.Error("error sending message", "error", err)
					continue
				}
			}

			m.metrics.Duration(fmt.Sprintf(`alerts_dispatched_duration_seconds{provider="%s", room="%s"}`, m.ID(), m.Room()), now)
		}
	}

	return nil
}

// Room returns the name of room for which this provider is configured.
func (m *GoogleChatManager) Room() string {
	return m.room
}

// ID returns the provider name.
func (m *GoogleChatManager) ID() string {
	return "google_chat"
}
