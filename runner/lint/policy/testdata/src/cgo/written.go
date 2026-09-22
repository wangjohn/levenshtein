// Package cgo is hand-written; the analyzers see cgo's rewrite of this file,
// which opens with a generated header.
package cgo

/*
static int twice(int value) { return 2 * value; }
*/
import "C"

// Twice doubles a value in C.
func Twice(value int) int { // want "function Twice"
	return int(C.twice(C.int(value)))
}
