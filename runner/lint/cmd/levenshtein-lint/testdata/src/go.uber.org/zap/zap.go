// Package zap stands in for zap's sugared logger.
package zap

type SugaredLogger struct{}

func (s *SugaredLogger) Infow(message string, keysAndValues ...any) {}
