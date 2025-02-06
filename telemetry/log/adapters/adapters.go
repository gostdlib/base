// Package adapters provides adapters for converting logging instances to the *slog.Logger type.
// This allows existing logging packages that are in use to be used with our logging package.
package adapters

import (
	"log/slog"

	"github.com/gostdlib/base/telemetry/log"

	"github.com/rs/zerolog"
	slogzap "github.com/samber/slog-zap/v2"
	slogzerolog "github.com/samber/slog-zerolog/v2"
	"go.uber.org/zap"
)

// Zap creates a new slog.Logger that writes to a Zap logger.
func Zap(l *zap.Logger) *slog.Logger {
	return slog.New(
		slogzap.Option{
			AddSource: true,
			Level:     log.LogLevel,
			Logger:    l,
		}.NewZapHandler())
}

// ZeroLog creates a new slog.Logger that writes to a Zerolog logger.
func ZeroLog(l zerolog.Logger) *slog.Logger {
	return slog.New(
		slogzerolog.Option{
			AddSource: true,
			Level:     log.LogLevel,
			Logger:    &l,
		}.NewZerologHandler())
}
