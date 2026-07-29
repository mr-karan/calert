package google_chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"unicode/utf8"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	chatv1 "google.golang.org/api/chat/v1"
)

const maxMsgSize = 4096

// prepareMessage renders text and CardsV2 independently. Only text is split
// to maxMsgSize bytes; CardsV2 has no verified size limit in this client.
func (m *GoogleChatManager) prepareMessage(alert any) ([]chatv1.Message, error) {
	var toText, toCard bytes.Buffer

	if err := m.msgTmpl.Execute(&toText, alert); err != nil {
		m.lo.Error("error parsing values in template", "error", err)
		return nil, err
	}
	if m.msgTmpl.Lookup("cardsV2") != nil {
		if err := m.msgTmpl.ExecuteTemplate(&toCard, "cardsV2", alert); err != nil {
			m.lo.Error("error parsing values in template", "error", err)
			return nil, err
		}
	}

	messages := make([]chatv1.Message, 0)
	if toText.Len() > 0 {
		for _, text := range splitText(withLegacyNewline(toText.String()), maxMsgSize) {
			messages = append(messages, chatv1.Message{Text: text})
		}
	}
	if toCard.Len() > 0 {
		var card chatv1.CardWithId
		if err := json.Unmarshal(toCard.Bytes(), &card); err != nil {
			m.lo.Error("error unmarshalling card message", "error", err)
			return nil, err
		}
		if len(messages) == 0 {
			messages = append(messages, chatv1.Message{})
		}
		messages[len(messages)-1].CardsV2 = []*chatv1.CardWithId{&card}
	}
	return messages, nil
}

// withLegacyNewline preserves the historic trailing newline without creating
// a newline-only chunk when rendered text is exactly a message boundary.
func withLegacyNewline(text string) string {
	if len(text)%maxMsgSize == 0 {
		return text
	}
	return text + "\n"
}

func splitText(text string, limit int) []string {
	if text == "" {
		return nil
	}
	chunks := make([]string, 0, (len(text)+limit-1)/limit)
	start, size := 0, 0
	for index, r := range text {
		runeSize := utf8.RuneLen(r)
		if size > 0 && size+runeSize > limit {
			chunks = append(chunks, text[start:index])
			start, size = index, 0
		}
		size += runeSize
	}
	if start < len(text) {
		chunks = append(chunks, text[start:])
	}
	return chunks
}

// sendMessage pushes out a notification to Google Chat space.
func (m *GoogleChatManager) sendMessage(ctx context.Context, msg chatv1.Message, threadKey string, threaded bool) error {
	out, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encoding Google Chat message")
	}

	u, err := url.Parse(m.endpoint)
	if err != nil {
		return fmt.Errorf("building Google Chat request")
	}
	q := u.Query()
	q.Set("messageReplyOption", "MESSAGE_REPLY_OPTION_UNSPECIFIED")
	if threaded {
		q.Set("messageReplyOption", "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
		q.Set("threadKey", threadKey)
	}
	u.RawQuery = q.Encode()

	hasCard := len(msg.CardsV2) > 0
	m.lo.Debug("sending alert", "threaded", threaded, "has_cards_v2", hasCard, "payload_bytes", len(out))
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("building Google Chat request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return fmt.Errorf("sending Google Chat request")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google chat returned status %d", resp.StatusCode)
	}
	return nil
}
