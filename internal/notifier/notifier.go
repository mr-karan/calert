package notifier

import (
	"fmt"
	"log/slog"
	"strings"

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

	if _, ok := n.providers[room]; !ok {
		availableRooms := make([]string, 0, len(n.providers))
		for r := range n.providers {
			availableRooms = append(availableRooms, r)
		}

		n.lo.Error("no provider available for room",
			"room", room,
			"available_rooms", availableRooms,
		)

		hint := ""
		if strings.Contains(room, "/") {
			hint = " (hint: Kubernetes AlertmanagerConfig prefixes receiver with namespace/config-name - use ?room_name= query param to override)"
		}

		return fmt.Errorf("no provider configured for room: %s, available: %v%s", room, availableRooms, hint)
	}

	n.providers[room].Push(alerts)
	return nil
}
