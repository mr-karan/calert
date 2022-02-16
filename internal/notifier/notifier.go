package notifier

import (
	"fmt"

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
	// Initialise a map with room as the key and their corresponding providers.
	m := make(map[string]providers.Provider, 0)

	for _, prov := range opts.Providers {
		m[prov.GetRoom()] = prov
	}

	return Notifier{
		lo:        opts.Log,
		providers: m,
	}, nil
}

// Dispatch pushes out a notification to Google Chat Room.
func (n *Notifier) Dispatch(alerts []alertmgrtmpl.Alert) error {
	// Group alerts based on their room names.
	alertsByRoom := make(map[string][]alertmgrtmpl.Alert, 0)

	n.lo.WithField("alerts_len", len(alerts)).Info("dispatching alerts")

	for _, a := range alerts {
		room := a.Labels["room"]
		alertsByRoom[room] = append(alertsByRoom[room], a)
	}
	// For each room, dispatch the alert based on their provider.
	for k, v := range alertsByRoom {
		// Do a lookup for the provider by the room name and push the alerts.
		// TODO: Check if the key is here.
		fmt.Println(k, v)
		// n.providers[k].Push(v)
	}
	return nil
}
