package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	alerttemplate "github.com/prometheus/alertmanager/template"
)

const (
	statusSuccess = "success"
	statusError   = "error"

	generalErrorMsg = "Something went wrong"

	exceptData      = "DataException"
	exceptNetwork   = "NetworkException"
	excepBadRequest = "InputException"
	excepGeneral    = "GeneralException"
)

// ErrorType defines string error constants (eg: DataException)
// to be sent with JSON responses.
type ErrorType string

// apiResponse is used to send uniform response structure.
type apiResponse struct {
	Status    string      `json:"status"`
	Message   string      `json:"message,omitempty"`
	Data      interface{} `json:"data"`
	ErrorType ErrorType   `json:"error_type,omitempty"`
}

// sendEnvelope is used to send success response based on format defined in apiResponse
func sendEnvelope(w http.ResponseWriter, data interface{}, message string) {
	// Standard marshalled envelope for success.
	a := apiResponse{
		Status:  statusSuccess,
		Data:    data,
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(a)
	if err != nil {
		errLog.Panicf("Quitting %s", err)
	}
}

// sendErrorEnvelope is used to send error response based on format defined in apiResponse
func sendErrorEnvelope(w http.ResponseWriter, code int, message string, data interface{}, et ErrorType) {
	// Standard marshalled envelope for error.
	a := apiResponse{
		Status:    statusError,
		Message:   message,
		Data:      data,
		ErrorType: et,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	err := json.NewEncoder(w).Encode(a)
	if err != nil {
		errLog.Panicf("Quitting %s", err)
	}
}

func handleIndex(a *App, w http.ResponseWriter, r *http.Request) (code int, msg string, data interface{}, et ErrorType, err error) {
	var (
		message = "Welcome to Alertmanager - GChat bot API"
	)
	return 200, message, nil, "", nil
}

func handleNewAlert(a *App, w http.ResponseWriter, r *http.Request) (code int, msg string, data interface{}, et ErrorType, err error) {
	var (
		alertData = alerttemplate.Data{}
		n         = a.notifier
	)
	// decode request payload from Alertmanager in a struct
	if err := json.NewDecoder(r.Body).Decode(&alertData); err != nil {
		errMsg := fmt.Sprintf("Error while decoding alertmanager response: %s", err)
		return http.StatusBadRequest, errMsg, nil, excepBadRequest, err
	}
	// send notification to chat
	err = sendMessageToChat(alertData.Alerts, &n)
	if err != nil {
		return http.StatusInternalServerError, "Something went wrong while sending alert notification", nil, excepGeneral, err
	}
	return http.StatusOK, "Alert sent", nil, "", nil
}

func handleHealthCheck(a *App, w http.ResponseWriter, r *http.Request) (code int, msg string, data interface{}, et ErrorType, err error) {
	var (
		message = ""
		health  = HealthCheckOutputSeriailizer{
			Ping:         "pong",
			BuildVersion: version,
			BuildDate:    date,
		}
	)
	return http.StatusOK, message, health, "", nil
}
