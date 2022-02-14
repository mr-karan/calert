package notifier

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mr-karan/calert/internal/providers"
)

// Notifier represents an instance that pushes out notifications to
// upstream providers.
type Notifier struct {
	client    *http.Client
	providers []providers.Provider
}

type Opts struct {
	MaxIdleConn int
	Timeout     time.Duration
	ProxyURL    string
	Providers   []providers.Provider
}

// Init initialises a new instance of the Notifier.
func Init(opts Opts) (Notifier, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost: opts.MaxIdleConn,
	}

	if opts.ProxyURL != "" {
		u, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return Notifier{}, fmt.Errorf("error parsing proxy URL: %s", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}

	// Generic HTTP Client for communicating with the upstream provider endpoint.
	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
	}

	return Notifier{
		client:    client,
		providers: opts.Providers,
	}, nil
}

// Dispatch pushes out a notification to Google Chat Room.
// func (n *Notifier) Dispatch(notif interface{}) error {
// 	out, err := json.Marshal(notif)
// 	if err != nil {
// 		return err
// 	}
// 	// prepare POST request to webhook endpoint
// 	req, err := http.NewRequest("POST", webHookURL, bytes.NewBuffer(out))
// 	if err != nil {
// 		return err
// 	}
// 	req.Header.Set("Content-Type", "application/json")

// 	// send the request
// 	resp, err := n.httpClient.Do(req)
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()

// 	// if response is not 200 log error response from gchat
// 	if resp.StatusCode != http.StatusOK {
// 		respMsg, _ := ioutil.ReadAll(resp.Body)
// 		errLog.Printf("Error sending alert Webhook API error: %s", string(respMsg))
// 		return errors.New("Error while sending alert")
// 	}

// 	return nil
// }
