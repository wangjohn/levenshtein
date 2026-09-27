package buildreceivers

import "testing"

func (t *Tally) reset() {
	t.count = 0
}

func TestReset(t *testing.T) {
	tally := Tally{count: 1}
	tally.reset()
	if tally.Count() != 0 {
		t.Error("reset kept the count")
	}
}
