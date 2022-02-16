package providers

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// GoogleChatManager represents the various methods for interacting with Google Chat.
type GoogleChatManager struct {
	endpoint string
	room     string
	client   *http.Client
}

type GoogleChatOpts struct {
	MaxIdleConn int
	Timeout     time.Duration
	ProxyURL    string
	Endpoint    string
	Room        string
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

	return &GoogleChatManager{
		client:   client,
		endpoint: opts.Endpoint,
		room:     opts.Room,
	}, nil
}

// Push sends out events to an HTTP Endpoint.
func (m *GoogleChatManager) Push(alerts []alertmgrtmpl.Alert) error {
	fmt.Println("got alerts", len(alerts))
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
