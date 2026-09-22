package keyvalues

import (
	"log/slog"

	"go.uber.org/zap"
)

type name string

func (n name) String() string {
	return string(n)
}

func Pairs(logger *zap.SugaredLogger, user *name) {
	logger.Infow("retrying", "attempt") // want "odd number of arguments"
	logger.Infow("retrying", "user", user)
	slog.Info("retrying", "attempt")
}
