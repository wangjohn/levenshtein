// Package notests has no tests, so Staticcheck lints it once, and that build
// alone judges its directives.
package notests

// Steady takes values everywhere.
//
//lint:ignore recvcheck nothing mixes receivers here
type Steady struct {
	n int
}

// N returns the value.
func (s Steady) N() int {
	return s.n
}
