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

	// init router
	r := mux.NewRouter()
	r.HandleFunc("/", handleIndex).Methods("GET")
	r.HandleFunc("/create", func(w http.ResponseWriter, r *http.Request) {
		handleNewAlert(w, r, &notifier)
	}).Methods("POST")
	// TODO : r.HandleFunc("/metrics", handleMetrics).Methods("GET")
	r.HandleFunc("/ping", handleHealthCheck).Methods("GET")

	// Initialize HTTP server and pass router
	s := &http.Server{
		Addr:         viper.GetString("server.address"),
		Handler:      r,
		ReadTimeout:  time.Millisecond * viper.GetDuration("server.read_timeout"),
		WriteTimeout: time.Millisecond * viper.GetDuration("server.write_timeout"),
	}

	sysLog.Printf("listening on %s | %s", viper.GetString("server.address"), viper.GetString("server.socket"))
	if err := s.ListenAndServe(); err != nil {
		errLog.Println("error starting server:", err)
	}

	sysLog.Println("bye")

}

func main() {
	initPackage()
}
