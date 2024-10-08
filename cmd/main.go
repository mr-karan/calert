package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mr-karan/calert/internal/metrics"
	"github.com/mr-karan/calert/internal/notifier"
)

var (
	// Version of the build. This is injected at build-time.
	buildString = "unknown"
)

// App is the global contains
// instances of various objects used in the lifecyle of program.
type App struct {
	lo       *slog.Logger
	metrics  *metrics.Manager
	notifier notifier.Notifier
}

func main() {
	// Initialise and load the config.
	ko, err := initConfig("config.sample.toml", "CALERT_")
	if err != nil {
		panic(err.Error())
	}

	var (
		metrics = initMetrics()
	)

	// Initialise logger.
	verbose := false
	if ko.String("app.log") == "debug" {
		verbose = true
	}
	lo := initLogger(verbose)

	// Initialise providers.
	provs, err := initProviders(ko, lo, metrics)
	if err != nil {
		lo.Error("error initialising providers", "error", err)
		exit()
	}

	// Initialise notifier.
	notifier, err := initNotifier(ko, lo, provs)
	if err != nil {
		lo.Error("error initialising notifier", "error", err)
		exit()
	}

	app := &App{
		lo:       lo,
		notifier: notifier,
		metrics:  metrics,
	}

	app.lo.Info("starting calert", "version", buildString, "verbose", verbose)

	// Initialise HTTP Router.
	r := chi.NewRouter()

	// Add some middlewares.
	r.Use(middleware.RequestID)
	if ko.Bool("app.enable_request_logs") {
		r.Use(middleware.Logger)
	}

	// Register Handlers
	r.Get("/", wrap(app, handleIndex))
	r.Get("/ping", wrap(app, handleHealthCheck))
	r.Get("/metrics", wrap(app, handleMetrics))
	r.Post("/dispatch", wrap(app, handleDispatchNotif))

	// Start HTTP Server.
	app.lo.Info("starting http server", "address", ko.MustString("app.address"))
	srv := &http.Server{
		// http.Server does not support structured logging. The best we can do
		// is to use our structured logger at a fixed log level. The "msg" field
		// will contain the whole log message, but at least any error log from
		// http.Server will be a JSON object, as the rest of the logs.
		ErrorLog:     slog.NewLogLogger(lo.Handler(), slog.LevelError),
		Addr:         ko.MustString("app.address"),
		ReadTimeout:  ko.MustDuration("app.server_timeout"),
		WriteTimeout: ko.MustDuration("app.server_timeout"),
		Handler:      r,
	}
	if err := srv.ListenAndServe(); err != nil {
		app.lo.Error("couldn't start server", "error", err)
		exit()
	}
}

func exit() {
	os.Exit(1)
}
