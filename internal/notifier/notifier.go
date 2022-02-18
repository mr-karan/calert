package notifier

import (
	"github.com/mr-karan/calert/internal/providers"
	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/sirupsen/logrus"
)

// Notifier represents an instance that pushes out notifications to
// upstream providers.
type Notifier struct {
	providers map[string]providers.Provider
	lo        *logrus.Logger
}

type Opts struct {
	Providers []providers.Provider
	Log       *logrus.Logger
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
func (n *Notifier) Dispatch(alerts []alertmgrtmpl.Alert) error {
	n.lo.WithField("count", len(alerts)).Info("dispatching alerts")

	// Group alerts based on their room names.
	alertsByRoom := make(map[string][]alertmgrtmpl.Alert, 0)
	for _, a := range alerts {
		room := a.Labels["room"]
		alertsByRoom[room] = append(alertsByRoom[room], a)
	}

	// For each room, dispatch the alert based on their provider.
	for k, v := range alertsByRoom {
		// Lookup for the provider by the room name.
		if _, ok := n.providers[k]; !ok {
			n.lo.WithField("room", k).Warn("no provider available for room")
			continue
		}
		// Push the batch of alerts.
		n.providers[k].Push(v)
	}

	return nil
}