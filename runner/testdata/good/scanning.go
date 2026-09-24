package good

import (
	"bufio"
	"io"
)

// CountLines reports a read error or an over-long line instead of a short
// count, so scannererr has nothing to report.
func CountLines(in io.Reader) (int, error) {
	lines := 0
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return lines, nil
}

// Shape is a sum type whose switches list every variant.
//
//sumtype:decl
type Shape interface {
	sealed()
}

// Circle is one Shape variant.
type Circle struct {
	Radius float64
}

func (Circle) sealed() {}

// Square is another Shape variant.
type Square struct {
	Side float64
}

func (Square) sealed() {}

// Area lists both variants, so gochecksumtype passes it.
func Area(shape Shape) float64 {
	switch shape := shape.(type) {
	case Circle:
		return shape.Radius * shape.Radius * 3
	case Square:
		return shape.Side * shape.Side
	}
	return 0
}
