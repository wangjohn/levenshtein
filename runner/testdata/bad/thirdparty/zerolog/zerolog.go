// Package zerolog is a local stand-in for zerolog, so the fixture can exercise
// zerologlint without resolving zerolog over the network.
package zerolog

import (
	"fmt"
	"io"
)

// Logger writes events to an output.
type Logger struct {
	out io.Writer
}

// Event is one log entry, written when it is dispatched.
type Event struct {
	out    io.Writer
	fields string
}

func New(out io.Writer) Logger {
	return Logger{out: out}
}

func (l *Logger) Info() *Event {
	return &Event{out: l.out}
}

func (e *Event) Str(key, value string) *Event {
	e.fields += fmt.Sprintf(" %s=%s", key, value)
	return e
}

func (e *Event) Msg(message string) {
	fmt.Fprintln(e.out, message+e.fields)
}

func (e *Event) Send() {
	e.Msg("")
}
