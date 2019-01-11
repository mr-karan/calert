package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
)

// Notifier represents an instance that pushes out notifications to
// Google Chat Webhook endpoint.
type Notifier struct {
	root       string
	httpClient *http.Client
}

// NewNotifier initialises a new instance of the Notifier.
func NewNotifier(h http.Client) Notifier {
	return Notifier{
		httpClient: &h,
	}
}

// PushNotification pushes out a notification to Google Chat Room.
func (n *Notifier) PushNotification(notif ChatNotification, webHookURL string) error {
	out, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	// prepare POST request to webhook endpoint
	req, err := http.NewRequest("POST", webHookURL, bytes.NewBuffer(out))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// send the request
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// if response is not 200 log error response from gchat
	if resp.StatusCode != http.StatusOK {
		respMsg, _ := ioutil.ReadAll(resp.Body)
		errLog.Printf("Error sending alert Webhook API error: %s", string(respMsg))
		return errors.New("Error while sending alert")
	}

	return nil
}
