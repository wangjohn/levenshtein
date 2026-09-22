// Package sum has a test file that does not compile, so go-test is an error:
// its tests never ran.
package sum

// Add returns the sum of a and b.
func Add(a, b int) int { return a + b }
