package google_chat

import (
	"fmt"
	"log/slog"
)

// slogAdapter implements the retryablehttp.LeveledLogger interface using slog.
type slogAdapter struct {
	logger *slog.Logger
}

// Implements for the retryablehttp.LeveledLogger
// ref. https://pkg.go.dev/github.com/hashicorp/go-retryablehttp#LeveledLogger
func (adpt *slogAdapter) Error(msg string, keysAndValues ...interface{}) {
	adpt.logger.Error(msg, keysAndValues...)
}

func (adpt *slogAdapter) Info(msg string, keysAndValues ...interface{}) {
	adpt.logger.Info(msg, keysAndValues...)
}

func (adpt *slogAdapter) Debug(msg string, keysAndValues ...interface{}) {
	adpt.logger.Debug(msg, keysAndValues...)
}

func (adpt *slogAdapter) Warn(msg string, keysAndValues ...interface{}) {
	adpt.logger.Warn(msg, keysAndValues...)
}

// Implements for the retryablehttp.Logger
// ref. https://pkg.go.dev/github.com/hashicorp/go-retryablehttp#Logger
func (adpt *slogAdapter) Printf(format string, args ...interface{}) {
	adpt.logger.Info(fmt.Sprintf(format, args...))
}
