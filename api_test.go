package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestIndexHandler(t *testing.T) {
	var (
		httpClient = initClient()
		notifier   = NewNotifier(*httpClient)
		appConfig  = &App{notifier}
	)

	// Create a request to pass to our handler.
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	handler := http.Handler(Handler{appConfig, handleIndex})

	// handler satisfy http.Handler since ServeHTTP is implemented
	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestHealthCheckHandler(t *testing.T) {
	var (
		httpClient = initClient()
		notifier   = NewNotifier(*httpClient)
		appConfig  = &App{notifier}
	)

	// Create a request to pass to our handler.
	req, err := http.NewRequest("GET", "/ping", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	handler := http.Handler(Handler{appConfig, handleHealthCheck})

	// handler satisfy http.Handler since ServeHTTP is implemented
	handler.ServeHTTP(rr, req)
	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestNewAlertHandler(t *testing.T) {
	var (
		httpClient = initClient()
		notifier   = NewNotifier(*httpClient)
		appConfig  = &App{notifier}
	)

	// Create a request to pass to our handler.
	// Load mock payload
	configFile, err := ioutil.ReadFile("examples/mock_payload.json")
	if err != nil {
		log.Fatal("Error opening config file", err.Error())
		os.Exit(1)
	}

	req, err := http.NewRequest("POST", "/create?room_name=alertManagerTestRoom", bytes.NewBuffer(configFile))
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	handler := http.Handler(Handler{appConfig, handleNewAlert})

	// handler satisfy http.Handler since ServeHTTP is implemented
	handler.ServeHTTP(rr, req)
	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}
