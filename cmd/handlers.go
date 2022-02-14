package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// wrap is a middleware that wraps HTTP handlers and injects the "app" context.
func wrap(app *App, next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "app", app)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resp is used to send uniform response structure.
type resp struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// sendResponse sends a JSON envelope to the HTTP response.
func sendResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out, err := json.Marshal(resp{Status: "success", Data: data})

	if err != nil {
		sendErrorResponse(w, "Internal Server Error.", http.StatusInternalServerError, nil)
		return
	}

	w.Write(out)
}

// sendErrorResponse sends a JSON error envelope to the HTTP response.
func sendErrorResponse(w http.ResponseWriter, message string, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	resp := resp{Status: "error",
		Message: message,
		Data:    data}
	out, _ := json.Marshal(resp)

	w.Write(out)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	sendResponse(w, "welcome to cAlerts!")
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	sendResponse(w, "pong")
}

// func handleNewAlert(a *App, w http.ResponseWriter, r *http.Request) (code int, msg string, data interface{}, et ErrorType, err error) {
// 	var (
// 		alertData = alerttemplate.Data{}
// 		n         = a.notifier
// 	)
// 	// decode request payload from Alertmanager in a struct
// 	if err := json.NewDecoder(r.Body).Decode(&alertData); err != nil {
// 		errMsg := fmt.Sprintf("Error while decoding alertmanager response: %s", err)
// 		return http.StatusBadRequest, errMsg, nil, excepBadRequest, err
// 	}
// 	// fetch the room_name param. This room_name is used to map the webhook URL from the config.
// 	// just an abstraction, for a more humanised version and to not end up making alertmanager config
// 	// a mess by not flooding with google chat webhook URLs all over the place.
// 	roomName := r.URL.Query().Get("room_name")
// 	if roomName == "" {
// 		// Attempt to fetch the room name from the alert payload
// 		roomName = alertData.Alerts[0].Labels["room_name"]
// 		if roomName == "" {
// 			return http.StatusBadRequest, "Missing required room_name param", nil, excepBadRequest, err
// 		}
// 	}
// 	webHookURL := viper.GetString(fmt.Sprintf("app.chat.%s.notification_url", roomName))
// 	if webHookURL == "" {
// 		errMsg := fmt.Sprintf("Webhook URL not configured for room_name: %s", roomName)
// 		return http.StatusBadRequest, errMsg, nil, excepBadRequest, err
// 	}
// 	// send notification to chat
// 	err = sendMessageToChat(alertData.Alerts, &n, webHookURL)
// 	if err != nil {
// 		return http.StatusInternalServerError, "Something went wrong while sending alert notification", nil, excepGeneral, err
// 	}
// 	return http.StatusOK, "Alert sent", nil, "", nil
// }
