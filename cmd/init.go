package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/mr-karan/calert/internal/notifier"
	prvs "github.com/mr-karan/calert/internal/providers"
	"github.com/mr-karan/calert/internal/providers/google_chat"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

// initLogger initializes logger instance.
func initLogger(ko *koanf.Koanf) *logrus.Logger {
	logger := logrus.New()

	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	// Enable debug mode if specified.
	if ko.String("app.log") == "debug" {
		logger.SetLevel(logrus.DebugLevel)
	}

	return logger
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
	err = ko.Load(file.Provider(*cfgPath), toml.Parser())
	if err != nil {
		return nil, err
	}

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
func initProviders(ko *koanf.Koanf, lo *logrus.Logger) []prvs.Provider {
	provs := make([]prvs.Provider, 0)

	// Loop over all providers listed in config.
	for _, name := range ko.MapKeys("providers") {
		cfgKey := fmt.Sprintf("providers.%s", name)
		provType := ko.String(fmt.Sprintf("%s.type", cfgKey))

		switch provType {
		case "google_chat":
			gchat, err := google_chat.NewGoogleChat(
				google_chat.GoogleChatOpts{
					Log:             lo,
					Timeout:         ko.MustDuration(fmt.Sprintf("%s.timeout", cfgKey)),
					MaxIdleConn:     ko.MustInt(fmt.Sprintf("%s.max_idle_conns", cfgKey)),
					ProxyURL:        ko.String(fmt.Sprintf("%s.proxy_url", cfgKey)),
					Endpoint:        ko.MustString(fmt.Sprintf("%s.endpoint", cfgKey)),
					Room:            name,
					Template:        ko.MustString(fmt.Sprintf("%s.template", cfgKey)),
					ActiveAlertsTTL: ko.MustDuration(fmt.Sprintf("%s.active_alerts_ttl", cfgKey)),
				},
			)
			if err != nil {
				lo.WithError(err).Fatal("error initialising google chat provider")
			}

			// Start a background worker to cleanup alerts based on TTL mechanism.
			go gchat.InitPruner(1 * time.Hour)

			lo.WithField("room", gchat.GetRoom()).Info("initialised provider")
			provs = append(provs, gchat)
		}
	}

	if len(provs) == 0 {
		lo.Fatal("no providers listed in config")
	}

	return provs
}

// initNotifier initializes a Notifier instance.
func initNotifier(ko *koanf.Koanf, lo *logrus.Logger, provs []prvs.Provider) notifier.Notifier {
	n, err := notifier.Init(notifier.Opts{
		Providers: provs,
		Log:       lo,
	})
	if err != nil {
		lo.WithError(err).Fatal("error initialising notifier")
	}

	return n
}
