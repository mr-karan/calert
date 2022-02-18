package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/sirupsen/logrus"
)

const (
	maxMsgSize = 4096
)

// ActiveAlerts represents a map of alerts unique fingerprint hash
// with their details.
// We store this map to use "threading" in Google Chat.
// Alertmanager doesn't send a unique ID either, so we need to prune
// the alerts based on a TTL from a config and hence we need a lock here.
type ActiveAlerts struct {
	sync.RWMutex
	alerts map[string]AlertDetails
}

type AlertDetails struct {
	StartsAt time.Time
	UUID     uuid.UUID
}

type ChatMessage struct {
	Text string `json:"text"`
}

// add adds an alert to the active alerts map.
func (d *ActiveAlerts) add(a alertmgrtmpl.Alert) error {
	d.Lock()
	defer d.Unlock()

	// Create a UUID for the alert. This UUID is
	// sent as a `threadKey` param in G-Chat API.
	// Set UUID for the alert.
	uid, err := uuid.NewV4()
	if err != nil {
		return err
	}

	// Add the alert metadata to the map.
	d.alerts[a.Fingerprint] = AlertDetails{
		UUID:     uid,
		StartsAt: a.StartsAt,
	}

	return nil
}

// remove removes the alert from the active alerts map.
func (d *ActiveAlerts) remove(fingerprint string) {
	d.Lock()
	defer d.Unlock()
	delete(d.alerts, fingerprint)
}

// loookup retrievs the UUID for the alert based on the fingerprint.
func (d *ActiveAlerts) loookup(fingerprint string) string {
	d.Lock()
	defer d.Unlock()
	// Do a lookup for the provider by the room name and push the alerts.
	if _, ok := d.alerts[fingerprint]; !ok {
		return ""
	}
	return d.alerts[fingerprint].UUID.String()
}

// GoogleChatManager represents the various methods for interacting with Google Chat.
type GoogleChatManager struct {
	lo           *logrus.Logger
	activeAlerts ActiveAlerts
	endpoint     string
	room         string
	client       *http.Client
	msgTmpl      *template.Template
}

type GoogleChatOpts struct {
	Log         *logrus.Logger
	MaxIdleConn int
	Timeout     time.Duration
	ProxyURL    string
	Endpoint    string
	Room        string
	Template    string
}

// NewGoogleChat initializes a Google Chat provider object.
func NewGoogleChat(opts GoogleChatOpts) (*GoogleChatManager, error) {
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("google_chat provider misconfigured. Missing required value: `endpoint`")
	}

	if opts.Room == "" {
		return nil, fmt.Errorf("google_chat provider misconfigured. Missing required value: `room`")
	}

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

	a := ActiveAlerts{
		alerts: alerts,
	}

	// common funcs used in template
	templateFuncMap := template.FuncMap{
		"Title":    strings.Title,
		"toUpper":  strings.ToUpper,
		"Contains": strings.Contains,
	}
	// read template file
	tmpl, err := template.New("message.tmpl").Funcs(templateFuncMap).ParseFiles(opts.Template)
	if err != nil {
		return nil, err
	}

	return &GoogleChatManager{
		lo:           opts.Log,
		client:       client,
		endpoint:     opts.Endpoint,
		room:         opts.Room,
		activeAlerts: a,
		msgTmpl:      tmpl,
	}, nil
}

// Push sends out events to an HTTP Endpoint.
func (m *GoogleChatManager) Push(alerts []alertmgrtmpl.Alert) error {
	m.lo.WithField("count", len(alerts)).Info("dispatching alerts to google chat")
	// For each alert, lookup the UUID and send the alert.
	for _, a := range alerts {
		if m.activeAlerts.loookup(a.Fingerprint) == "" {
			m.activeAlerts.add(a)
		}
		msgs, err := m.prepareMessage(a)
		if err != nil {
			m.lo.WithError(err).Error("error preparing message")
			continue
		}
		// If message is split in multiple parts
		// due to size limit, send as individual HTTP requests.
		for _, msg := range msgs {
			err := m.sendMessage(msg, m.activeAlerts.alerts[a.Fingerprint].UUID.String())
			if err != nil {
				m.lo.WithError(err).Error("error sending message")
				continue
			}
		}
	}
	return nil
}

// Name returns the notification provider name.
func (m *GoogleChatManager) GetRoom() string {
	return m.room
}

// ID returns the notification provider name.
func (m *GoogleChatManager) ID() string {
	return "google_chat"
}

func (m *GoogleChatManager) prepareMessage(alert alertmgrtmpl.Alert) ([]ChatMessage, error) {
	var (
		str strings.Builder
		to  bytes.Buffer
		msg ChatMessage
	)
	messages := make([]ChatMessage, 0)

	err := m.msgTmpl.Execute(&to, alert)
	if err != nil {
		m.lo.WithError(err).Error("Error parsing values in template")
		return messages, err
	}
	if (len(str.String()) + len(to.String())) >= maxMsgSize {
		msg.Text = str.String()
		messages = append(messages, msg)
		str.Reset()
	}
	str.WriteString(to.String())
	str.WriteString("\n")

	// prepare request payload for Google chat webhook endpoint
	msg.Text = str.String()
	messages = append(messages, msg)

	return messages, nil
}

// PushNotification pushes out a notification to Google Chat Room.
func (m *GoogleChatManager) sendMessage(msg ChatMessage, threadKey string) error {
	out, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// Set threadkey in the URL
	u, err := url.Parse(m.endpoint)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("threadKey", threadKey)
	u.RawQuery = q.Encode()
	endpoint := u.String()

	// prepare POST request to webhook endpoint.
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(out))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// send the request
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// if response is not 200 log error response from gchat
	if resp.StatusCode != http.StatusOK {
		m.lo.WithField("status", resp.StatusCode).Error("Non OK HTTP Response received from Google Chat Webhook endpoint")
		return errors.New("non ok response from gchat")
	}

	return nil
}
