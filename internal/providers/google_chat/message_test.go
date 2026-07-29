package google_chat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mr-karan/calert/internal/metrics"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	chatv1 "google.golang.org/api/chat/v1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSplitTextBoundariesAndUTF8(t *testing.T) {
	exact := strings.Repeat("a", maxMsgSize)
	chunks := splitText(exact, maxMsgSize)
	require.Equal(t, []string{exact}, chunks)
	assert.Equal(t, exact, withLegacyNewline(exact), "exact boundary must not gain a newline-only chunk")
	assert.Equal(t, "ordinary\n", withLegacyNewline("ordinary"))

	oversize := strings.Repeat("🙂", 1100)
	chunks = splitText(oversize, maxMsgSize)
	require.Greater(t, len(chunks), 1)
	assert.Equal(t, oversize, strings.Join(chunks, ""))
	for _, chunk := range chunks {
		assert.NotEmpty(t, chunk)
		assert.LessOrEqual(t, len(chunk), maxMsgSize)
		assert.True(t, utf8.ValidString(chunk))
	}
}

func TestPrepareMessageDoesNotAddEmptyTextMessage(t *testing.T) {
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Metrics:  metrics.New("calert"),
		Endpoint: "http://test",
		Room:     "room",
		Template: "../../../static/message-card.tmpl",
	})
	require.NoError(t, err)

	alert := alertmgrtmpl.Alert{Labels: alertmgrtmpl.KV{"severity": "warn\"\\\n", "alertname": "name\"\\\n"}, Status: "firing", Annotations: alertmgrtmpl.KV{"summary": "quote \" newline\n slash\\"}}
	messages, err := chat.prepareMessage(alert)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Empty(t, messages[0].Text)
	assert.Len(t, messages[0].CardsV2, 1)
}

func TestSendMessageSanitizesTransportAndResponseErrors(t *testing.T) {
	var logs bytes.Buffer
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Metrics:  metrics.New("calert"),
		Endpoint: "https://chat.example.test/webhook?key=key-sentinel&token=token-sentinel",
		Room:     "room",
		Template: "../../../static/message.tmpl",
	})
	require.NoError(t, err)
	chat.client.HTTPClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport-token-sentinel")
	})

	err = chat.sendMessage(context.Background(), chatv1.Message{Text: "test"}, "thread", true)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sentinel")
	assert.NotContains(t, logs.String(), "sentinel")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("response-body-sentinel"))
	}))
	defer srv.Close()
	chat.endpoint = srv.URL + "/webhook?token=token-sentinel"
	chat.client.HTTPClient.Transport = nil
	err = chat.sendMessage(context.Background(), chatv1.Message{Text: "test"}, "thread", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
	assert.NotContains(t, err.Error(), "sentinel")
	assert.NotContains(t, logs.String(), "response-body-sentinel")
	assert.NotContains(t, logs.String(), "token-sentinel")
}
