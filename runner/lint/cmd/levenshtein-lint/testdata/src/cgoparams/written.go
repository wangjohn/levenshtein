// Package cgoparams is hand-written; the analyzers see cgo's rewrite of this
// file, which opens with a generated header.
package cgoparams

/*
static int twice(int value) { return 2 * value; }
*/
import "C"

func double(value int, label string) int { // want "double - label is unused"
	return int(C.twice(C.int(value)))
}

// Double calls double so it is not dead code.
func Double(value int, label string) int {
	return double(value, label)
}
