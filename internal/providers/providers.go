package providers

import (
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// WebhookPayload is the JSON object Alertmanager posts to webhook receivers:
// the template Data plus the envelope fields (notably GroupKey) that are not
// part of template.Data itself.
type WebhookPayload struct {
	alertmgrtmpl.Data

	Version         string `json:"version"`
	GroupKey        string `json:"groupKey"`
	TruncatedAlerts uint64 `json:"truncatedAlerts"`
}

type Provider interface {
	// ID represents the name of provider.
	ID() string
	// Room returns the room name specified for the provider.
	Room() string
	// Push pushes the notification for the full Alertmanager webhook
	// payload to the upstream provider.
	Push(payload WebhookPayload) error
}
