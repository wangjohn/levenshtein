// Package shapes computes areas. Scale is gone and Area returns a new type,
// both of which break importers.
package shapes

// Square is a square with sides of one length.
type Square struct {
	Side float64
}

// Area returns the square's area, rounded down.
func (s Square) Area() int {
	return int(s.Side * s.Side)
}
