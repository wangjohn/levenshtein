// Package zap is a local stand-in for zap's sugared logger, so the fixture can
// exercise loggercheck without resolving zap over the network.
package zap

// SugaredLogger records a message with alternating keys and values.
type SugaredLogger struct {
	sink func(message string, keysAndValues ...any)
}

func NewSugared(sink func(message string, keysAndValues ...any)) *SugaredLogger {
	return &SugaredLogger{sink: sink}
}

func (s *SugaredLogger) Infow(message string, keysAndValues ...any) {
	s.sink(message, keysAndValues...)
}
