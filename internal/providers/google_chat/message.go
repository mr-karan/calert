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
func (m *GoogleChatManager) prepareMessage(alert alertmgrtmpl.Alert) ([]outerStruct, error) {
	var (
		to  bytes.Buffer
		msg outerStruct
	)

	messages := make([]outerStruct, 0)

	// Render a template with alert data.
	err := m.msgTmpl.Execute(&to, alert)
	if err != nil {
		m.lo.WithError(err).Error("Error parsing values in template")
		return messages, err
	}

	// Convert the template bytes to string.
	text := to.String()

	// Create a text paragraph widget for the message.
	widget := textParagraphWidget{Text: text{Text: text}}

	// Create a section with the widget.
	section := section{Widgets: []widget{widget}}

	// Create a header with the alert name and status.
	header := header{Title: fmt.Sprintf("%s: %s", alert.Status(), alert.Name())}

	// Create a card with the header and section.
	card := card{Header: header, Sections: []section{section}}

	// Create an outer struct with the card and a unique cardId.
	msg = outerStruct{
		Cards: []card{card},
		cardId: m.activeAlerts.alerts[alert.Fingerprint].UUID.String(),
	}

	// Add the message to batch.
	messages = append(messages, msg)

	return messages, nil
}

// sendMessage pushes out a notification to Google Chat space.
func (m *GoogleChatManager) sendMessage(msg outerStruct) error {
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
	q.Set("threadKey", msg.cardId)
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
