package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	flag "github.com/spf13/pflag"

	"github.com/spf13/viper"
)

var (
	// Version of the build.
	// This is injected at build-time.
	// Be sure to run the provided run script to inject correctly.
	version = "unknown"
	date    = "unknown"
	sysLog  *log.Logger
	errLog  *log.Logger
)

// App is the context that's injected into HTTP request handlers.
type App struct {
	notifier Notifier
	sysLog   *log.Logger
}

// The Handler struct that takes App and a function matching
// our useful signature.
type Handler struct {
	*App
	H func(a *App, w http.ResponseWriter, r *http.Request) (code int, message string, data interface{}, et ErrorType, err error)
}

// ServeHTTP allows our Handler type to satisfy http.Handler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	code, msg, data, et, err := h.H(h.App, w, r)
	if err != nil {
		errLog.Printf("Error while processing request: %s", err)
		sendErrorEnvelope(w, code, msg, data, et)
	} else {
		sendEnvelope(w, data, msg)
	}
}

func initLogger() {
	sysLog = log.New(os.Stdout, "SYS: ", log.Ldate|log.Ltime|log.Llongfile)
	errLog = log.New(os.Stderr, "ERR: ", log.Ldate|log.Ltime|log.Llongfile)
}

func initConfig() {
	// Command line flags.
	flagSet := flag.NewFlagSet("config", flag.ContinueOnError)
	flagSet.Usage = func() {
		fmt.Println(flagSet.FlagUsages())
		os.Exit(0)
	}

	viper.SetConfigName("config")
	viper.SetDefault("server.address", ":5000")
	viper.SetDefault("server.socket", "/tmp/calert.sock")
	viper.SetDefault("server.name", "calert")
	viper.SetDefault("server.read_timeout", 1000)
	viper.SetDefault("server.write_timeout", 5000)
	viper.SetDefault("server.keepalive_timeout", 30000)
	viper.SetDefault("server.max_body_size", 5000)

	// Process flags.
	flagSet.Parse(os.Args[1:])
	viper.BindPFlags(flagSet)

	// Config file.
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		errLog.Fatalf("Error reading config: %s", err)
	}
}

// Package initialisation.
func initPackage() {
	initLogger()
	initConfig()

	// Generic HTTP handler for communicating with the Chat webhook endpoint.
	httpClient := &http.Client{
		Timeout: time.Duration(viper.GetDuration("app.http_client.request_timeout") * time.Millisecond),
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   viper.GetInt("app.http_client.max_idle_conns"),
			ResponseHeaderTimeout: time.Duration(viper.GetDuration("app.http_client.request_timeout") * time.Millisecond),
		}}

	// Notifier for sending alerts.
	notifier := NewNotifier(viper.GetString("app.notification_url"), *httpClient)

	context := &App{notifier, sysLog}

	// init router
	r := mux.NewRouter()
	r.Handle("/", Handler{context, handleIndex}).Methods("GET")
	r.Handle("/create", Handler{context, handleNewAlert}).Methods("POST")
	r.Handle("/ping", Handler{context, handleHealthCheck}).Methods("GET")
	// TODO : r.HandleFunc("/metrics", handleMetrics).Methods("GET")

	// Initialize HTTP server and pass router
	s := &http.Server{
		Addr:         viper.GetString("server.address"),
		Handler:      r,
		ReadTimeout:  time.Millisecond * viper.GetDuration("server.read_timeout"),
		WriteTimeout: time.Millisecond * viper.GetDuration("server.write_timeout"),
	}

	// Start the web server
	sysLog.Printf("listening on %s | %s", viper.GetString("server.address"), viper.GetString("server.socket"))
	if err := s.ListenAndServe(); err != nil {
		errLog.Fatalf("error starting server: %s", err)
	}

}

func main() {
	initPackage()
}
