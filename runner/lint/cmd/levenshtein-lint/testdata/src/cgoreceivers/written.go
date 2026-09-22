// Package cgoreceivers is hand-written; the analyzers see cgo's rewrite of
// this file, which opens with a generated header.
package cgoreceivers

/*
static int twice(int value) { return 2 * value; }
*/
import "C"

type widget struct { // want `the methods of "widget" use pointer receiver and non-pointer receiver.`
	value int
}

func (w widget) Twice() int {
	return int(C.twice(C.int(w.value)))
}

func (w *widget) Set(value int) {
	w.value = value
}

// Module takes pointer receivers here; generated.go adds a value receiver
// that recvcheck must not count.
type Module struct {
	calls int
}

// Call records one call.
func (m *Module) Call() int {
	m.calls++
	return m.calls
}
