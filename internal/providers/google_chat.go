package providers

import (
	"fmt"
)

type GoogleChatNotif struct {
	Msg string `json:"text"`
}

// GoogleChatManager represents the various methods for interacting with Google Chat.
type GoogleChatManager struct {
	rootURL string
}

// NewGoogleChat initializes a Google Chat provider object.
func NewGoogleChat(rootURL string) (*GoogleChatManager, error) {
	if rootURL == "" {
		return nil, fmt.Errorf("google_chat provider misconfigured. Missing required value: `root_url`")
	}

	return &GoogleChatManager{
		rootURL: rootURL,
	}, nil
}

// Push sends out events to an HTTP Endpoint.
func (m *GoogleChatManager) Push(notif interface{}) error {
	// req, err := http.NewRequest("POST", m.rootURL, bytes.NewBuffer(data))
	// if err != nil {
	// 	m.log.WithError(err).Error("error preparing http request")
	// 	return err
	// }
	// req.Header.Set("Content-Type", "application/json")

	// resp, err := m.client.Do(req)
	// if err != nil {
	// 	m.log.WithError(err).Error("error sending http request")
	// 	return err
	// }
	// defer resp.Body.Close()

	// if resp.StatusCode != http.StatusOK {
	// 	return fmt.Errorf("received non 200 OK from upstream: %s", resp.Status)
	// }

	return nil
}

// Name returns the notification provider name.
func (m *GoogleChatManager) ID() string {
	return "google_chat"
}
