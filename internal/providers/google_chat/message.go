package google_chat

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

const (
	maxMsgSize = 4096
)

// prepareMessage accepts an Alert object and templates out with the user provided template.
// It also splits the alerts if the combined size exceeds the limit of 4096 bytes by
// G-Chat Webhook API
func (m *GoogleChatManager) prepareMessage(alert alertmgrtmpl.Alert) ([]ChatMessage, error) {
	var (
		str strings.Builder
		to  bytes.Buffer
		msg ChatMessage
	)

	messages := make([]ChatMessage, 0)

	// Render a template with alert data.
	err := m.msgTmpl.Execute(&to, alert)
	if err != nil {
		m.lo.WithError(err).Error("Error parsing values in template")
		return messages, err
	}

	// Split the message if it exceeds the limit.
	if (len(str.String()) + len(to.String())) >= maxMsgSize {
		msg.Text = str.String()
		messages = append(messages, msg)
		str.Reset()
	}

	// Convert the template bytes to string.
	str.WriteString(to.String())
	str.WriteString("\n")
	msg.Text = str.String()

	// Add the message to batch.
	messages = append(messages, msg)

	return messages, nil
}

// sendMessage pushes out a notification to Google Chat space.
func (m *GoogleChatManager) sendMessage(msg ChatMessage, threadKey string) error {
	out, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Parse the webhook URL to add `?threadKey` param.
	u, err := url.Parse(m.endpoint)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("threadKey", threadKey)
	q.Set("messageReplyOption", "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
	u.RawQuery = q.Encode()
	endpoint := u.String()

	// Prepare the request.
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(out))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request.
	m.lo.WithField("url", endpoint).WithField("msg", msg.Text).Debug("sending alert")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// If response is non 200, log and throw the error.
	if resp.StatusCode != http.StatusOK {
		m.lo.WithField("status", resp.StatusCode).Error("Non OK HTTP Response received from Google Chat Webhook endpoint")
		return errors.New("non ok response from gchat")
	}

	return nil
}
