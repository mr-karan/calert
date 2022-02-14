package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi"
	"github.com/mr-karan/calert/internal/notifier"

	"github.com/sirupsen/logrus"
)

var (
	// Version of the build. This is injected at build-time.
	buildString = "unknown"
)

// App is the global container that holds
// objects of various routines that run on boot.
type App struct {
	log      *logrus.Logger
	notifier notifier.Notifier
}

func main() {
	// Initialise and load the config.
	ko, err := initConfig("config.sample.toml", "CALERTS_")
	if err != nil {
		fmt.Println("error initialising config", err)
		os.Exit(1)
	}
	// Initialise Logger.
	log := initLogger(ko)

	provs, err := initProviders(ko)
	if err != nil {
		log.WithError(err).Fatal("error initialising notifier")
	}
	// Initialise Notifier.
	notifier, err := initNotifier(ko, provs)
	if err != nil {
		log.WithError(err).Fatal("error initialising notifier")
	}

	// Initialise a new instance of app.
	app := &App{
		log:      log,
		notifier: notifier,
	}

	// Start an instance of app.
	app.log.WithField("version", buildString).Info("booting calerts")

	// Initialise HTTP Router.
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("welcome to cAlerts!"))
	})

	// r.Post("/dispatch", wrap(app, handleDispatchNotif))
	r.Get("/ping", wrap(app, handleHealthCheck))
	//TODO: Add metrics

	// HTTP Server.

	srv := &http.Server{
		Addr:         ko.MustString("app.address"),
		ReadTimeout:  ko.MustDuration("app.server_timeout"),
		WriteTimeout: ko.MustDuration("app.server_timeout"),
		Handler:      r,
	}

	app.log.WithField("addr", buildString).Info("booting calerts")
	if err := srv.ListenAndServe(); err != nil {
		app.log.WithError(err).Fatal("couldn't start server")
	}
}
