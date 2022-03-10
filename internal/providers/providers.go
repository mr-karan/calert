package providers

import (
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

type Provider interface {
	// ID represents the name of provider.
	ID() string
	// Room returns the room name specified for the provider.
	Room() string
	// Push pushes the notification to upstream provider.
	Push(alerts []alertmgrtmpl.Alert) error
}
