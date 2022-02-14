package providers

type Provider interface {
	// ID represents the name of provider.
	ID() string
	// Push pushes the notification to upstream provider.
	Push(notif interface{}) error
}
