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
	// Be sure to run the provided run script to inject correctly (check Makefile).
	version = "unknown"
	date    = "unknown"
	sysLog  *log.Logger
	errLog  *log.Logger
)

// App is the context that's injected into HTTP request handlers.
type App struct {
	notifier Notifier
}

// The Handler struct takes App and a function matching
// our useful signature. It is used to pass App as context in handlers
type Handler struct {
	*App
	HandleRequest func(a *App, w http.ResponseWriter, r *http.Request) (code int, message string, data interface{}, et ErrorType, err error)
}

// ServeHTTP allows our Handler type to satisfy http.Handler so that
// Handler can be used with r.Handle.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	code, msg, data, et, err := h.HandleRequest(h.App, w, r)
	if et != "" {
		if err != nil {
			errLog.Printf("Error while processing request: %s", err)
		}
		sendErrorEnvelope(w, code, msg, data, et)
	} else {
		sendEnvelope(w, code, msg, data)
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

	// Config Path flag.
	flagSet.String("config.file", "", "Path to config file")

	viper.SetDefault("server.address", ":5000")
	viper.SetDefault("server.socket", "/tmp/calert.sock")
	viper.SetDefault("server.name", "calert")
	viper.SetDefault("server.read_timeout", 1000)
	viper.SetDefault("server.write_timeout", 5000)
	viper.SetDefault("server.keepalive_timeout", 30000)
	viper.SetDefault("app.max_size", 4000)
	// Process flags.
	flagSet.Parse(os.Args[1:])
	viper.BindPFlags(flagSet)

	// Config file.
	// check if config.file flag is passed and read from the file if it is set
	configPath := viper.GetString("config.file")
	if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		// fallback to default config.
		viper.SetConfigName("config")
		viper.AddConfigPath(".")
	}
	err := viper.ReadInConfig()
	if err != nil {
		errLog.Fatalf("Error reading config: %s", err)
	}
}

func initClient() *http.Client {
	// Generic HTTP handler for communicating with the Chat webhook endpoint.
	return &http.Client{
		Timeout: time.Duration(viper.GetDuration("app.http_client.request_timeout") * time.Millisecond),
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   viper.GetInt("app.http_client.max_idle_conns"),
			ResponseHeaderTimeout: time.Duration(viper.GetDuration("app.http_client.request_timeout") * time.Millisecond),
		}}
}

// prog initialisation.
func init() {
	initLogger()
	initConfig()
}

func main() {
	var (
		httpClient = initClient()
		notifier   = NewNotifier(*httpClient)
		appConfig  = &App{notifier}
	)

	// init router
	r := mux.NewRouter()
	r.Handle("/", Handler{appConfig, handleIndex}).Methods("GET")
	r.Handle("/create", Handler{appConfig, handleNewAlert}).Methods("POST")
	r.Handle("/ping", Handler{appConfig, handleHealthCheck}).Methods("GET")
	// TODO : r.HandleFunc("/metrics", handleMetrics).Methods("GET")

	// Initialize HTTP server and pass router
	s := &http.Server{
		Addr:         viper.GetString("server.address"),
		Handler:      r,
		ReadTimeout:  time.Millisecond * viper.GetDuration("server.read_timeout"),
		WriteTimeout: time.Millisecond * viper.GetDuration("server.write_timeout"),
		IdleTimeout:  time.Millisecond * viper.GetDuration("server.keepalive_timeout"),
	}

	// Start the web server
	sysLog.Printf("listening on %s | %s", viper.GetString("server.address"), viper.GetString("server.socket"))
	if err := s.ListenAndServe(); err != nil {
		errLog.Fatalf("error starting server: %s", err)
	}
}
