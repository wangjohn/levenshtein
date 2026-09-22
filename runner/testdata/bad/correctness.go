package bad

import (
	"encoding/json"
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

type setting struct {
	Name  string
	Value int
}

// musttag: untagged fields take their JSON keys from the Go identifiers, so
// renaming a field silently changes which key it reads.
func Musttag(data []byte) (setting, error) {
	var value setting
	err := json.Unmarshal(data, &value)
	return value, err
}

// Counter mixes value and pointer receivers.
type Counter struct {
	hits int
}

// recvcheck: Hits copies the whole value while Record changes the original, so
// a Counter value and a *Counter have different method sets.
func (c Counter) Hits() int {
	return c.hits
}

// Record keeps the pointer receiver that makes the receivers mixed.
func (c *Counter) Record() {
	c.hits++
}
