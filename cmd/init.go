package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/mr-karan/calert/internal/notifier"
	prvs "github.com/mr-karan/calert/internal/providers"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

// initLogger initializes logger.
func initLogger(ko *koanf.Koanf) *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})
	if ko.String("app.log") == "debug" {
		logger.SetLevel(logrus.DebugLevel)
	}
	return logger
}

// initConfig loads config to `ko`
// object.
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

func initProviders(ko *koanf.Koanf) ([]prvs.Provider, error) {
	provs := make([]prvs.Provider, 0)

	for _, prov := range ko.Slices("notifier.providers") {
		switch prov.String("type") {
		case "google_chat":
			gchat, err := prvs.NewGoogleChat(prov.String("endpoint"))
			if err != nil {
				return nil, err
			}
			provs = append(provs, gchat)
		}
	}
	return provs, nil
}

// initNotifier initializes a Notifier instance.
func initNotifier(ko *koanf.Koanf, provs []prvs.Provider) (notifier.Notifier, error) {
	notify, err := notifier.Init(notifier.Opts{
		Timeout:     ko.MustDuration("notifier.timeout"),
		MaxIdleConn: ko.MustInt("notifier.max_idle_conns"),
		ProxyURL:    ko.String("notifier.proxy_url"),
		Providers:   provs,
	})
	if err != nil {
		return notifier.Notifier{}, err
	}

	return notify, nil
}
