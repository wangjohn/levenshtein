// Package sum has a test that fails, so go-test reports it as a finding with
// go test's own output.
package sum

// Add returns the sum of a and b.
func Add(a, b int) int { return a + b }
