package main

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
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
	lo       *logrus.Logger
	notifier notifier.Notifier
}

func main() {
	// Initialise and load the config.
	ko, err := initConfig("config.sample.toml", "CALERTS_")
	if err != nil {
		// Need to use `panic` since logger can only be initialised once config is initialised.
		panic(err.Error())
	}
	var (
		lo       = initLogger(ko)
		provs    = initProviders(ko, lo)
		notifier = initNotifier(ko, lo, provs)
	)

	// Initialise a new instance of app.
	app := &App{
		lo:       lo,
		notifier: notifier,
	}

	// Start an instance of app.
	app.lo.WithField("version", buildString).Info("booting calerts")

	// Initialise HTTP Router.
	r := chi.NewRouter()

	// Add some middlewares.
	r.Use(middleware.RequestID)
	if ko.Bool("app.enable_request_logs") {
		r.Use(middleware.Logger)
	}

	r.Get("/", wrap(app, handleIndex))
	r.Get("/ping", wrap(app, handleHealthCheck))
	// TODO: Add metrics handler.
	// r.Get("/metrics", wrap(app, handleMetrics))
	r.Post("/dispatch", wrap(app, handleDispatchNotif))

	// Start HTTP Server.
	srv := &http.Server{
		Addr:         ko.MustString("app.address"),
		ReadTimeout:  ko.MustDuration("app.server_timeout"),
		WriteTimeout: ko.MustDuration("app.server_timeout"),
		Handler:      r,
	}

	app.lo.WithField("addr", ko.MustString("app.address")).Info("starting http server")
	if err := srv.ListenAndServe(); err != nil {
		app.lo.WithError(err).Fatal("couldn't start server")
	}
}
