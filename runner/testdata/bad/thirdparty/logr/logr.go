// Package logr is a local stand-in for logr, so the fixture can exercise
// loggercheck without resolving logr over the network.
package logr

// Logger records a message with alternating keys and values.
type Logger struct {
	sink func(message string, keysAndValues ...any)
}

func New(sink func(message string, keysAndValues ...any)) Logger {
	return Logger{sink: sink}
}

func (l Logger) Info(message string, keysAndValues ...any) {
	l.sink(message, keysAndValues...)
}
