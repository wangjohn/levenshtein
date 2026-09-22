// Package cgo sits apart from package bad because cgo imports unsafe, and
// unusedwrite skips every package that does.
package cgo

// cgo marks its rewrite of this file generated, and the linter sees only the
// rewrite, so both findings below must still be reported against this
// hand-written original.

/*
static int triple(int value) { return 3 * value; }
*/
import "C"

// SA4006: the first result is overwritten before anything reads it.
func Triple(value int) int {
	tripled := int(C.triple(C.int(value)))
	tripled = 3 * value
	return tripled
}

// Unit is an enum.
type Unit int

// The units.
const (
	Meter Unit = iota
	Foot
)

// exhaustive: the switch leaves Foot out, and exhaustive skips a file with a
// generated header unless told otherwise.
func Scale(unit Unit, value int) int {
	switch unit {
	case Meter:
		return int(C.triple(C.int(value)))
	}
	return value
}
