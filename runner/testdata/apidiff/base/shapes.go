// Package shapes computes areas.
package shapes

// Square is a square with sides of one length.
type Square struct {
	Side float64
}

// Area returns the square's area.
func (s Square) Area() float64 {
	return s.Side * s.Side
}

// Scale multiplies a length by a factor.
func Scale(length, factor float64) float64 {
	return length * factor
}
