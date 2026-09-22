package bad

import (
	"errors"
	"io"
	"os"
	"time"
)

type node struct {
	value int
}

// nilness: the guarded branch dereferences a pointer it just proved nil.
func Nilness(n *node) int {
	if n == nil {
		return n.value
	}
	return n.value
}

type point struct {
	x int
	y int
}

// unusedwrite: the range variable is a copy, so the write is lost.
func Unusedwrite(points []point) int {
	total := 0
	for _, p := range points {
		p.x = 1
		total += p.y
	}
	return total
}

// errorlint: a wrapped error never compares equal with ==.
func Errorlint(err error) bool {
	return err == os.ErrNotExist
}

func work() error {
	return nil
}

// nilerr: the branch proves the error is not nil and then discards it.
func Nilerr() error {
	if err := work(); err != nil {
		return nil
	}
	return nil
}

// durationcheck: multiplying two durations scales by nanoseconds.
func Durationcheck(wait time.Duration) time.Duration {
	return wait * time.Second
}

// reassign: replacing another package's sentinel breaks every comparison.
func Reassign() {
	io.EOF = errors.New("replaced")
}

// ineffassign and wastedassign: the first value is never read.
func Ineffassign() int {
	value := 1
	value = 2
	return value
}
