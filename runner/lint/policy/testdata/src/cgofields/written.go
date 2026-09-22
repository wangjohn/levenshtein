// Package cgofields is hand-written; the analyzers see cgo's rewrite of this
// file, which opens with a generated header.
package cgofields

/*
static int twice(int value) { return 2 * value; }
*/
import "C"

type pair struct {
	left, right int // want "declare each struct field on its own line"
}

// Twice doubles both values in C.
func Twice(values pair) pair {
	return pair{
		left:  int(C.twice(C.int(values.left))),
		right: int(C.twice(C.int(values.right))),
	}
}
