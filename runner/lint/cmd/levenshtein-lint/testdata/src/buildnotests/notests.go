// Package buildnotests has no tests, so its only build judges its directives
// and reports nothing under one that matches no finding.
package buildnotests

//lint:ignore recvcheck nothing mixes receivers here
type Steady struct {
	count int
}

func (s Steady) Count() int {
	return s.count
}
