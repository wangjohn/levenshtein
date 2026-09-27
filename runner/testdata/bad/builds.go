package bad

// This package has tests, so Staticcheck lints it with and without them. A
// directive that matches nothing in either build must still be reported.

// Steady takes values everywhere, in this file and in the tests.
//
//lint:ignore recvcheck nothing mixes receivers here
type Steady struct {
	n int
}

// N returns the value.
func (s Steady) N() int {
	return s.n
}

//lint:ignore unparam every parameter is used
func triple(value int) int {
	return value * 3
}

// Tripled uses triple.
func Tripled() int {
	return triple(1) + triple(2)
}
