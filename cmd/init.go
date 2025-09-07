package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/mr-karan/calert/internal/metrics"
	"github.com/mr-karan/calert/internal/notifier"
	prvs "github.com/mr-karan/calert/internal/providers"
	"github.com/mr-karan/calert/internal/providers/google_chat"
	flag "github.com/spf13/pflag"
)

// initLogger initializes logger instance.
func initLogger(verbose bool) *slog.Logger {
	lvl := slog.LevelInfo
	if verbose {
		lvl = slog.LevelDebug
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	}))
}

// initConfig loads config to `ko` object.
func initConfig(cfgDefault string, envPrefix string) (*koanf.Koanf, error) {
	var (
		ko = koanf.New(".")
		f  = flag.NewFlagSet("front", flag.ContinueOnError)
	)

	// Configure Flags.
	f.Usage = func() {
		fmt.Println(f.FlagUsages())
		os.Exit(0)
	}

	// Register `--config` flag.
	cfgPath := f.String("config", cfgDefault, "Path to a config file to load.")

	// Parse and Load Flags.
	err := f.Parse(os.Args[1:])
	if err != nil {
		return nil, err
	}

	// Load the config files from the path provided.
	log.Printf("attempting to load config from file: %s\n", *cfgPath)

	err = ko.Load(file.Provider(*cfgPath), toml.Parser())
	if err != nil {
		// If the default config is not present, print a warning and continue reading the values from env.
		if *cfgPath == cfgDefault {
			log.Printf("unable to open config file: %w falling back to env vars\n", err.Error())
		} else {
			return nil, err
		}
	}

	log.Println("attempting to read config from env vars")
	// Load environment variables if the key is given
	// and merge into the loaded config.
	if envPrefix != "" {
		err = ko.Load(env.Provider(envPrefix, ".", func(s string) string {
			return strings.Replace(strings.ToLower(
				strings.TrimPrefix(s, envPrefix)), "__", ".", -1)
		}), nil)
		if err != nil {
			return nil, err
		}
	}

	return ko, nil
}

// initProviders loads all the providers specified in the config.
func initProviders(ko *koanf.Koanf, lo *slog.Logger, metrics *metrics.Manager) ([]prvs.Provider, error) {
	provs := make([]prvs.Provider, 0)
	provDefOpts := map[string]interface{}{
		"max_idle_conns":   50,
		"timeout":          "30s",
		"template":         "static/message.tmpl",
		"thread_ttl":       "12h",
		"threaded_replies": false,
		"dry_run":          false,
		"retry_max":        3,
		"retry_wait_min":   "1s",
		"retry_wait_max":   "5s",
	}

	// Loop over all providers listed in config.
	for _, name := range ko.MapKeys("providers") {
		cfgKey := fmt.Sprintf("providers.%s", name)
		provType := ko.String(fmt.Sprintf("%s.type", cfgKey))

		// Set default values for the provider if not set in config.
		for valKey, defaultVal := range provDefOpts {
			if !ko.Exists(fmt.Sprintf("%s.%s", cfgKey, valKey)) {
				ko.Set(fmt.Sprintf("%s.%s", cfgKey, valKey), defaultVal)
			}
		}

		switch provType {
		case "google_chat":
			opts := google_chat.GoogleChatOpts{
				Log:             lo,
				Timeout:         ko.MustDuration(fmt.Sprintf("%s.timeout", cfgKey)),
				MaxIdleConn:     ko.MustInt(fmt.Sprintf("%s.max_idle_conns", cfgKey)),
				ProxyURL:        ko.String(fmt.Sprintf("%s.proxy_url", cfgKey)),
				Endpoint:        ko.MustString(fmt.Sprintf("%s.endpoint", cfgKey)),
				Room:            name,
				Template:        ko.MustString(fmt.Sprintf("%s.template", cfgKey)),
				ThreadTTL:       ko.MustDuration(fmt.Sprintf("%s.thread_ttl", cfgKey)),
				ThreadedReplies: ko.Bool(fmt.Sprintf("%s.threaded_replies", cfgKey)),
				Metrics:         metrics,
				DryRun:          ko.Bool(fmt.Sprintf("%s.dry_run", cfgKey)),
				RetryMax:        ko.Int(fmt.Sprintf("%s.retry_max", cfgKey)),
				RetryWaitMin:    ko.Duration(fmt.Sprintf("%s.retry_wait_min", cfgKey)),
				RetryWaitMax:    ko.Duration(fmt.Sprintf("%s.retry_wait_max", cfgKey)),
			}
			lo.Debug("provider options", "options", opts)

			gchat, err := google_chat.NewGoogleChat(opts)
			if err != nil {
				return nil, fmt.Errorf("error initialising google chat provider: %s", err)
			}

			lo.Info("initialised provider", "room", gchat.Room())
			provs = append(provs, gchat)
		}
	}

	if len(provs) == 0 {
		return nil, fmt.Errorf("no providers listed in config")
	}

	return provs, nil
}

// initNotifier initializes a Notifier instance.
func initNotifier(ko *koanf.Koanf, lo *slog.Logger, provs []prvs.Provider) (notifier.Notifier, error) {
	n, err := notifier.Init(notifier.Opts{
		Providers: provs,
		Log:       lo,
	})
	if err != nil {
		return notifier.Notifier{}, fmt.Errorf("error initialising notifier: %s", err)
	}

	return n, err
}

// initMetrics initializes a Metrics manager.
func initMetrics() *metrics.Manager {
	return metrics.New("calert")
}
