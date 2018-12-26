package main

type (
	// HealthCheckOutputSeriailizer defines the fields used to send a health check response
	HealthCheckOutputSeriailizer struct {
		BuildVersion string `json:"buildVersion"`
		BuildDate    string `json:"buildDate"`
		Ping         string `json:"ping"`
	}

	// ChatNotification defines the fiels required to send to Chat Webhook Endpoint
	ChatNotification struct {
		Text string `json:"text"`
	}
)
