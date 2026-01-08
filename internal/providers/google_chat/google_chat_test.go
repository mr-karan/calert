package google_chat

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
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
