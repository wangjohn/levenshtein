package cgoformat

/*
static int twice(int value) { return 2 * value; }
*/
import "C"

// Twice is gofmt-formatted, although cgo's rewrite of it is not.
func Twice(value int) int {
	return int(C.twice(C.int(value)))
}
