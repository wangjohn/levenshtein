package bad

import (
	"log/slog"
	"os"

	"github.com/go-logr/logr"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
)

// zerologlint: the event is built but never sent with Msg or Send, so nothing
// is logged.
func Zerologlint() {
	logger := zerolog.New(os.Stderr)
	logger.Info().Str("user", "ada")
}

// loggercheck: the last key has no value, so the logger drops it or pairs it
// with the wrong value.
func Loggercheck(logger logr.Logger, sugared *zap.SugaredLogger, attempt int) {
	logger.Info("retrying", "attempt")
	sugared.Infow("retrying", "attempt", attempt, "user")
}

// sloglint: key-value pairs and attributes mixed in one call.
func Sloglint(attempt int) {
	slog.Info("retrying", "attempt", attempt, slog.String("user", "ada"))
}
