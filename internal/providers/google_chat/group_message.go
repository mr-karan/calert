package google_chat

import (
	"sort"

	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// GroupOverflow summarises alerts that were not rendered as sections because
// the payload exceeded the per-message cap. Header counters stay exact since
// they are computed from the full payload, not from rendered sections.
type GroupOverflow struct {
	Count    int
	Firing   int
	Resolved int
}

// GroupTemplateContext is the template contract for group threading mode:
// one aggregated context per Alertmanager webhook payload.
type GroupTemplateContext struct {
	AlertName     string
	Status        string
	Labels        alertmgrtmpl.KV
	FiringCount   int
	ResolvedCount int
	Alerts        []alertmgrtmpl.Alert
	Overflow      GroupOverflow
	TrackingKey   string
	GeneratorURL  string
	Annotations   alertmgrtmpl.KV
}

// buildGroupContext converts a webhook payload into a group template context:
// exact firing/resolved counters, alerts ordered firing-first and capped at
// maxAlerts rendered entries, severity fallback to the first alert when it is
// not common to the whole group, and first-alert conveniences where no
// group-level equivalent exists.
//
// prevStatuses holds the fingerprint → status pairs of the previously posted
// message for this group (nil when unknown). A resolved instance is rendered
// only on its firing → resolved transition: once shown as resolved, it is
// dropped from later messages' sections. Header counters reflect the
// rendered set (plus overflow), so hidden instances are not counted.
func buildGroupContext(payload providers.WebhookPayload, trackingKey string, maxAlerts int, prevStatuses map[string]string) GroupTemplateContext {
	ctx := GroupTemplateContext{
		AlertName:   payload.GroupLabels["alertname"],
		Status:      payload.Status,
		TrackingKey: trackingKey,
	}

	// Copy CommonLabels so the severity fallback never mutates the payload.
	ctx.Labels = alertmgrtmpl.KV{}
	for k, v := range payload.CommonLabels {
		ctx.Labels[k] = v
	}

	// Render firing alerts always; render a resolved alert only if the
	// last posted message did not already show it as resolved.
	alerts := make([]alertmgrtmpl.Alert, 0, len(payload.Alerts))
	for _, a := range payload.Alerts {
		if a.Status != "firing" && prevStatuses[a.Fingerprint] == "resolved" {
			continue
		}
		alerts = append(alerts, a)
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		return alerts[i].Status == "firing" && alerts[j].Status != "firing"
	})

	// Count over the rendered set (post show-resolved-once filter) so the
	// header matches what's visible. Overflow items are still counted here
	// since they're surfaced in the overflow summary, not dropped.
	for _, a := range alerts {
		if a.Status == "firing" {
			ctx.FiringCount++
		} else {
			ctx.ResolvedCount++
		}
	}

	if len(alerts) > 0 {
		first := alerts[0]
		// Severity is not part of group_by, so it may be absent from
		// CommonLabels; fall back so icon logic never renders blank.
		if _, ok := ctx.Labels["severity"]; !ok {
			if sev, ok := first.Labels["severity"]; ok {
				ctx.Labels["severity"] = sev
			}
		}
		ctx.GeneratorURL = first.GeneratorURL
		ctx.Annotations = first.Annotations
	}

	if maxAlerts > 0 && len(alerts) > maxAlerts {
		for _, a := range alerts[maxAlerts:] {
			ctx.Overflow.Count++
			if a.Status == "firing" {
				ctx.Overflow.Firing++
			} else {
				ctx.Overflow.Resolved++
			}
		}
		alerts = alerts[:maxAlerts]
	}
	ctx.Alerts = alerts

	return ctx
}
