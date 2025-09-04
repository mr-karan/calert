package google_chat

import (
	"fmt"
	"log/slog"
)

type slogAdapter struct {
	logger *slog.Logger
}

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

func (adpt *slogAdapter) Printf(format string, args ...interface{}) {
	adpt.logger.Info(fmt.Sprintf(format, args...))
}
