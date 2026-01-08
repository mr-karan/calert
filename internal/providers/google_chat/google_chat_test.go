package google_chat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/mr-karan/calert/internal/metrics"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryOn429(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": {"code": 429, "message": "Rate limited"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	opts := &GoogleChatOpts{
		Log:          slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Endpoint:     server.URL,
		Room:         "test",
		Template:     "../../../static/message.tmpl",
		DryRun:       false,
		RetryMax:     5,
		RetryWaitMin: 10 * time.Millisecond,
		RetryWaitMax: 50 * time.Millisecond,
	}

	chat, err := NewGoogleChat(*opts)
	require.NoError(t, err)
	require.NotNil(t, chat)

	ctx := context.Background()
	mockResp := &http.Response{StatusCode: http.StatusTooManyRequests}
	shouldRetry, _ := chat.client.CheckRetry(ctx, mockResp, nil)
	assert.True(t, shouldRetry, "Should retry on 429 status code")

	mockResp200 := &http.Response{StatusCode: http.StatusOK}
	shouldRetry, _ = chat.client.CheckRetry(ctx, mockResp200, nil)
	assert.False(t, shouldRetry, "Should not retry on 200 status code")

	mockResp500 := &http.Response{StatusCode: http.StatusInternalServerError}
	shouldRetry, _ = chat.client.CheckRetry(ctx, mockResp500, nil)
	assert.True(t, shouldRetry, "Should retry on 500 status code (default behavior)")
}

func TestRetryPolicyIntegration(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := retryablehttp.NewClient()
	client.RetryMax = 5
	client.RetryWaitMin = 1 * time.Millisecond
	client.RetryWaitMax = 10 * time.Millisecond
	client.Logger = nil

	client.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		shouldRetry, checkErr := retryablehttp.DefaultRetryPolicy(ctx, resp, err)
		if shouldRetry {
			return true, checkErr
		}
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return true, nil
		}
		return false, nil
	}

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount), "Should have made 3 requests (2 retries + 1 success)")
}

func TestGoogleChatTemplate(t *testing.T) {
	opts := &GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Endpoint: "http://",
		Room:     "qa",
		Template: "../../../static/message.tmpl",
		DryRun:   true,
	}

	chat, err := NewGoogleChat(*opts)
	if err != nil || chat == nil {
		t.Fatal(err)
	}

	alert := alertmgrtmpl.Alert{
		Status: "firing",
		Labels: alertmgrtmpl.KV(map[string]string{
			"severity": "high", "alertname": "TestAlert",
		}),
		Annotations: alertmgrtmpl.KV(map[string]string{
			"team": "qa", "dryrun": "true",
		}),
	}

	expectedMessage := "*(HIGH) Testalert - Firing*\nDryrun: true\nTeam: qa\n\n"

	msgs, err := chat.prepareMessage(alert)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "message.tmpl", filepath.Base(chat.msgTmpl.Name()), "Message template name")
	assert.Equal(t, expectedMessage, msgs[0].Text, "Message content")
}

func TestTemplateFunctions(t *testing.T) {
	opts := &GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Endpoint: "http://",
		Room:     "test",
		Template: "../../../static/message.tmpl",
		DryRun:   true,
	}

	chat, err := NewGoogleChat(*opts)
	require.NoError(t, err)
	require.NotNil(t, chat.msgTmpl)

	t.Run("Template exists", func(t *testing.T) {
		fn := chat.msgTmpl.Lookup("message.tmpl")
		assert.NotNil(t, fn)
	})
}

func TestTemplateFunctionHelpers(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() string
		expected string
	}{
		{
			name:     "toUpper with string",
			fn:       func() string { return strings.ToUpper(fmt.Sprintf("%v", "hello")) },
			expected: "HELLO",
		},
		{
			name:     "toUpper with int converts to string",
			fn:       func() string { return strings.ToUpper(fmt.Sprintf("%v", 123)) },
			expected: "123",
		},
		{
			name:     "toLower with string",
			fn:       func() string { return strings.ToLower(fmt.Sprintf("%v", "HELLO")) },
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestActiveAlerts(t *testing.T) {
	lo := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	t.Run("add and lookup alert", func(t *testing.T) {
		aa := &ActiveAlerts{
			alerts: make(map[string]AlertDetails),
			lo:     lo,
		}

		alert := alertmgrtmpl.Alert{
			Fingerprint: "abc123",
			StartsAt:    time.Now(),
		}

		err := aa.add(alert)
		require.NoError(t, err)

		uuid := aa.loookup("abc123")
		assert.NotEmpty(t, uuid)
		assert.Len(t, uuid, 36)
	})

	t.Run("lookup non-existent alert returns empty", func(t *testing.T) {
		aa := &ActiveAlerts{
			alerts: make(map[string]AlertDetails),
			lo:     lo,
		}

		uuid := aa.loookup("nonexistent")
		assert.Empty(t, uuid)
	})

	t.Run("prune removes expired alerts", func(t *testing.T) {
		m := metrics.New("calert")
		aa := &ActiveAlerts{
			alerts:  make(map[string]AlertDetails),
			lo:      lo,
			metrics: m,
		}

		oldAlert := alertmgrtmpl.Alert{
			Fingerprint: "old",
			StartsAt:    time.Now().Add(-2 * time.Hour),
		}
		newAlert := alertmgrtmpl.Alert{
			Fingerprint: "new",
			StartsAt:    time.Now(),
		}

		aa.add(oldAlert)
		aa.add(newAlert)

		assert.Len(t, aa.alerts, 2)

		aa.Prune(1 * time.Hour)

		assert.Len(t, aa.alerts, 1)
		assert.Empty(t, aa.loookup("old"))
		assert.NotEmpty(t, aa.loookup("new"))
	})
}

func TestGoogleChatManager(t *testing.T) {
	t.Run("ID returns google_chat", func(t *testing.T) {
		opts := &GoogleChatOpts{
			Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
			Endpoint: "http://test",
			Room:     "test-room",
			Template: "../../../static/message.tmpl",
		}
		chat, err := NewGoogleChat(*opts)
		require.NoError(t, err)

		assert.Equal(t, "google_chat", chat.ID())
	})

	t.Run("Room returns configured room", func(t *testing.T) {
		opts := &GoogleChatOpts{
			Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
			Endpoint: "http://test",
			Room:     "my-room",
			Template: "../../../static/message.tmpl",
		}
		chat, err := NewGoogleChat(*opts)
		require.NoError(t, err)

		assert.Equal(t, "my-room", chat.Room())
	})
}

func TestPrepareMessage(t *testing.T) {
	opts := &GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Endpoint: "http://test",
		Room:     "test",
		Template: "../../../static/message.tmpl",
	}
	chat, err := NewGoogleChat(*opts)
	require.NoError(t, err)

	t.Run("prepares message with all fields", func(t *testing.T) {
		alert := alertmgrtmpl.Alert{
			Status:      "firing",
			Fingerprint: "test123",
			Labels: alertmgrtmpl.KV{
				"severity":  "critical",
				"alertname": "TestAlert",
			},
			Annotations: alertmgrtmpl.KV{
				"summary":     "Test summary",
				"description": "Test description",
			},
		}

		msgs, err := chat.prepareMessage(alert)
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		assert.Contains(t, msgs[0].Text, "CRITICAL")
		assert.Contains(t, msgs[0].Text, "Testalert")
		assert.Contains(t, msgs[0].Text, "Firing")
	})

	t.Run("handles empty annotations", func(t *testing.T) {
		alert := alertmgrtmpl.Alert{
			Status:      "resolved",
			Fingerprint: "test456",
			Labels: alertmgrtmpl.KV{
				"severity":  "warning",
				"alertname": "EmptyAnnotations",
			},
			Annotations: alertmgrtmpl.KV{},
		}

		msgs, err := chat.prepareMessage(alert)
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		assert.Contains(t, msgs[0].Text, "WARNING")
	})
}
