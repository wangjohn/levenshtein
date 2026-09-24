package bad

import (
	"bufio"
	"context"
	"io"
	"reflect"
	"strconv"
)

// nilnesserr: err is already known to be nil, so the second failure returns
// no error at all.
func Nilnesserr(first, second string) (int, error) {
	left, err := strconv.Atoi(first)
	if err != nil {
		return 0, err
	}
	right, secondErr := strconv.Atoi(second)
	if secondErr != nil {
		return 0, err
	}
	return left + right, nil
}

// fatcontext: each iteration wraps the previous context, so the chain grows
// with every loop.
func Fatcontext(ctx context.Context, keys []string) context.Context {
	for _, key := range keys {
		ctx = context.WithValue(ctx, contextKey(key), key)
	}
	return ctx
}

type contextKey string

// scannererr: a read error or an over-long line ends the loop early, and
// nothing checks scanner.Err, so the count comes back short with no error.
func Scannererr(in io.Reader) int {
	lines := 0
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		lines++
	}
	return lines
}

// reflectvaluecompare: == compares the reflect.Value headers, not the values
// they hold.
func Reflectvaluecompare(left, right any) bool {
	return reflect.ValueOf(left) == reflect.ValueOf(right)
}

// Shape is a sum type: every variant is declared in this package.
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

// gochecksumtype: the switch misses Square, and a default does not count.
func Gochecksumtype(shape Shape) float64 {
	switch shape := shape.(type) {
	case Circle:
		return shape.Radius * shape.Radius * 3
	default:
		return 0
	}
}
