package cgo

// A package that imports "C" is linted through cgo's rewrite of this file,
// which lives in the build cache and is marked generated. The file is
// gofmt-formatted, so LV1005 must judge this original rather than the rewrite.

/*
static int twice(int value) { return 2 * value; }
*/
import "C"

// Twice doubles a value in C.
func Twice(value int) int {
	return int(C.twice(C.int(value)))
}
