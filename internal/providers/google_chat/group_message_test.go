package google_chat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/calert/internal/metrics"
	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupAlert(fp, status, instance string) alertmgrtmpl.Alert {
	return alertmgrtmpl.Alert{
		Fingerprint: fp,
		Status:      status,
		StartsAt:    time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
		Labels: alertmgrtmpl.KV{
			"alertname": "PrometheusTargetMissing",
			"severity":  "warning",
			"instance":  instance,
		},
		Annotations:  alertmgrtmpl.KV{"summary": "target missing on " + instance},
		GeneratorURL: "https://prometheus.example.com/graph",
	}
}

func groupPayload(alerts ...alertmgrtmpl.Alert) providers.WebhookPayload {
	status := "resolved"
	for _, a := range alerts {
		if a.Status == "firing" {
			status = "firing"
		}
	}
	return providers.WebhookPayload{
		GroupKey: `{}/{}:{alertname="PrometheusTargetMissing"}`,
		Data: alertmgrtmpl.Data{
			Status:       status,
			GroupLabels:  alertmgrtmpl.KV{"alertname": "PrometheusTargetMissing"},
			CommonLabels: alertmgrtmpl.KV{"alertname": "PrometheusTargetMissing", "severity": "warning"},
			Alerts:       alerts,
		},
	}
}

func TestBuildGroupContext(t *testing.T) {
	t.Run("computes exact counters", func(t *testing.T) {
		ctx := buildGroupContext(groupPayload(
			groupAlert("a", "firing", "node1"),
			groupAlert("b", "resolved", "node2"),
			groupAlert("c", "firing", "node3"),
		), "tk", 10, nil)

		assert.Equal(t, 2, ctx.FiringCount)
		assert.Equal(t, 1, ctx.ResolvedCount)
		assert.Equal(t, "PrometheusTargetMissing", ctx.AlertName)
		assert.Equal(t, "firing", ctx.Status)
		assert.Equal(t, "tk", ctx.TrackingKey)
	})

	t.Run("orders alerts firing-first", func(t *testing.T) {
		ctx := buildGroupContext(groupPayload(
			groupAlert("a", "resolved", "node1"),
			groupAlert("b", "firing", "node2"),
			groupAlert("c", "resolved", "node3"),
		), "tk", 10, nil)

		require.Len(t, ctx.Alerts, 3)
		assert.Equal(t, "firing", ctx.Alerts[0].Status)
		// Resolved alerts keep their relative order (stable sort).
		assert.Equal(t, "a", ctx.Alerts[1].Fingerprint)
		assert.Equal(t, "c", ctx.Alerts[2].Fingerprint)
	})

	t.Run("caps rendered alerts with exact overflow summary", func(t *testing.T) {
		alerts := make([]alertmgrtmpl.Alert, 0, 15)
		for i := 0; i < 12; i++ {
			alerts = append(alerts, groupAlert(fmt.Sprintf("f%d", i), "firing", fmt.Sprintf("node%d", i)))
		}
		for i := 0; i < 3; i++ {
			alerts = append(alerts, groupAlert(fmt.Sprintf("r%d", i), "resolved", fmt.Sprintf("nodeR%d", i)))
		}

		ctx := buildGroupContext(groupPayload(alerts...), "tk", 10, nil)

		assert.Len(t, ctx.Alerts, 10)
		assert.Equal(t, 5, ctx.Overflow.Count)
		assert.Equal(t, 2, ctx.Overflow.Firing)
		assert.Equal(t, 3, ctx.Overflow.Resolved)
		// Counters stay exact regardless of capping.
		assert.Equal(t, 12, ctx.FiringCount)
		assert.Equal(t, 3, ctx.ResolvedCount)
	})

	t.Run("no overflow when under the cap", func(t *testing.T) {
		ctx := buildGroupContext(groupPayload(groupAlert("a", "firing", "node1")), "tk", 10, nil)
		assert.Len(t, ctx.Alerts, 1)
		assert.Equal(t, 0, ctx.Overflow.Count)
	})

	t.Run("severity falls back to first alert when not common", func(t *testing.T) {
		payload := groupPayload(
			groupAlert("a", "firing", "node1"),
			groupAlert("b", "firing", "node2"),
		)
		delete(payload.CommonLabels, "severity")

		ctx := buildGroupContext(payload, "tk", 10, nil)
		assert.Equal(t, "warning", ctx.Labels["severity"])
		// The payload itself is not mutated.
		_, ok := payload.CommonLabels["severity"]
		assert.False(t, ok)
	})

	t.Run("first-alert conveniences", func(t *testing.T) {
		ctx := buildGroupContext(groupPayload(
			groupAlert("a", "resolved", "node1"),
			groupAlert("b", "firing", "node2"),
		), "tk", 10, nil)

		// First alert after firing-first ordering is "b".
		assert.Equal(t, "https://prometheus.example.com/graph", ctx.GeneratorURL)
		assert.Equal(t, "target missing on node2", ctx.Annotations["summary"])
	})
}

func TestGroupTemplateRendersValidCardsV2(t *testing.T) {
	chat, err := NewGoogleChat(GoogleChatOpts{
		Log:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		Metrics:  metrics.New("calert"),
		Endpoint: "http://test",
		Room:     "test",
		Template: "../../../static/message-group.tmpl",
	})
	require.NoError(t, err)

	alerts := make([]alertmgrtmpl.Alert, 0, 12)
	for i := 0; i < 12; i++ {
		alerts = append(alerts, groupAlert(fmt.Sprintf("f%d", i), "firing", fmt.Sprintf("node%d", i)))
	}
	ctx := buildGroupContext(groupPayload(alerts...), "thread-key-123", 10, nil)

	msgs, err := chat.prepareMessage(ctx)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	require.Len(t, msgs[0].CardsV2, 1)
	card, err := json.Marshal(msgs[0].CardsV2[0])
	require.NoError(t, err)

	s := string(card)
	assert.Contains(t, s, "12 firing / 0 resolved")
	assert.Contains(t, s, "🟡 PrometheusTargetMissing")
	assert.Contains(t, s, "and 2 more instances (2 firing / 0 resolved)")
	assert.Contains(t, s, "Prometheus")
	assert.Contains(t, msgs[0].Text, "PrometheusTargetMissing")
}

func TestBuildGroupContextHidesAlreadyShownResolved(t *testing.T) {
	// Scenario from the field: node2 resolved and was shown as resolved in
	// the previous message; gitlab1 resolves next. The new message must
	// render gitlab1's transition but not re-show node2, and counters reflect
	// only the rendered set so the header matches what's visible.
	payload := groupPayload(
		groupAlert("a", "firing", "node1"),
		groupAlert("b", "resolved", "node2"),
		groupAlert("c", "resolved", "gitlab1"),
	)
	prev := map[string]string{"a": "firing", "b": "resolved", "c": "firing"}

	ctx := buildGroupContext(payload, "tk", 10, prev)

	require.Len(t, ctx.Alerts, 2)
	assert.Equal(t, "a", ctx.Alerts[0].Fingerprint)
	assert.Equal(t, "c", ctx.Alerts[1].Fingerprint)
	// Hidden node2 is not counted: header counts only the rendered set.
	assert.Equal(t, 1, ctx.FiringCount)
	assert.Equal(t, 1, ctx.ResolvedCount)

	t.Run("unknown previous status is rendered", func(t *testing.T) {
		ctx := buildGroupContext(payload, "tk", 10, map[string]string{"a": "firing"})
		assert.Len(t, ctx.Alerts, 3)
	})

	t.Run("nil previous statuses renders everything", func(t *testing.T) {
		ctx := buildGroupContext(payload, "tk", 10, nil)
		assert.Len(t, ctx.Alerts, 3)
	})
}
