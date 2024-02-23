package notifier

import (
	"fmt"
	"log/slog"

	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
)

// Notifier represents an instance that pushes out notifications to
// upstream providers.
type Notifier struct {
	providers map[string]providers.Provider
	lo        *slog.Logger
}

type Opts struct {
	Providers []providers.Provider
	Log       *slog.Logger
}

// Init initialises a new instance of the Notifier.
func Init(opts Opts) (Notifier, error) {
	// Initialise a map with room as the key and their corresponding provider instance.
	m := make(map[string]providers.Provider, 0)

	for _, prov := range opts.Providers {
		room := prov.Room()
		m[room] = prov
	}

	return Notifier{
		lo:        opts.Log,
		providers: m,
	}, nil
}

// Dispatch pushes out a notification to an upstream provider.
func (n *Notifier) Dispatch(alerts []alertmgrtmpl.Alert, room string) error {
	n.lo.Info("dispatching alerts", "count", len(alerts))

	// Lookup for the provider by the room name.
	if _, ok := n.providers[room]; !ok {
		n.lo.Error("no provider available for room", "room", room)
		return fmt.Errorf("no provider configured for room: %s", room)
	}
	// Push the batch of alerts.
	n.providers[room].Push(alerts)

	return nil
}
