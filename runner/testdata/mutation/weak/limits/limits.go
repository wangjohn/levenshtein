// Package limits is untested and unselected, so excluding it keeps it out of
// the report instead of showing up as uncovered.
package limits

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
