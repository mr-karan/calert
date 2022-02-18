package google_chat

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/sirupsen/logrus"
)

type GoogleChatManager struct {
	lo           *logrus.Logger
	activeAlerts *ActiveAlerts
	endpoint     string
	room         string
	client       *http.Client
	msgTmpl      *template.Template
}

type GoogleChatOpts struct {
	Log             *logrus.Logger
	MaxIdleConn     int
	Timeout         time.Duration
	ProxyURL        string
	Endpoint        string
	Room            string
	Template        string
	ActiveAlertsTTL time.Duration
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
		"Title":    strings.Title,
		"toUpper":  strings.ToUpper,
		"Contains": strings.Contains,
	}

	// Load the template.
	tmpl, err := template.New("message.tmpl").Funcs(templateFuncMap).ParseFiles(opts.Template)
	if err != nil {
		return nil, err
	}

	mgr := &GoogleChatManager{
		lo:       opts.Log,
		client:   client,
		endpoint: opts.Endpoint,
		room:     opts.Room,
		activeAlerts: &ActiveAlerts{
			alerts: alerts,
		},
		msgTmpl: tmpl,
	}
	// Start a background worker to cleanup alerts based on TTL mechanism.
	go mgr.activeAlerts.startPruneWorker(1*time.Hour, opts.ActiveAlertsTTL)

	return mgr, nil
}

// Push accepts the list of alerts and dispatches them to Webhook API endpoint.
func (m *GoogleChatManager) Push(alerts []alertmgrtmpl.Alert) error {
	m.lo.WithField("count", len(alerts)).Info("dispatching alerts to google chat")

	// For each alert, lookup the UUID and send the alert.
	for _, a := range alerts {
		// If it's a new alert whose fingerprint isn't in the active alerts map, add it first.
		if m.activeAlerts.loookup(a.Fingerprint) == "" {
			m.activeAlerts.add(a)
		}

		// Prepare a list of messages to send.
		msgs, err := m.prepareMessage(a)
		if err != nil {
			m.lo.WithError(err).Error("error preparing message")
			continue
		}

		// Dispatch an HTTP request for each message.
		for _, msg := range msgs {
			threadKey := m.activeAlerts.alerts[a.Fingerprint].UUID.String()

			// Send message to API.
			if err := m.sendMessage(msg, threadKey); err != nil {
				m.lo.WithError(err).Error("error sending message")
				continue
			}
		}
	}

	return nil
}

// GetRoom returns the name of room for which this provider is configured.
func (m *GoogleChatManager) GetRoom() string {
	return m.room
}

// ID returns the provider name.
func (m *GoogleChatManager) ID() string {
	return "google_chat"
}
