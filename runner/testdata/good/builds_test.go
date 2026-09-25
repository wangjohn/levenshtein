package good

import "testing"

func (t *Tally) reset() {
	t.count = 0
}

type fakeRegion struct{}

func (fakeRegion) region() {}

func TestBuilds(t *testing.T) {
	tally := Tally{count: 2}
	tally.reset()
	if tally.Count() != 0 {
		t.Errorf("Count() after reset = %d, want 0", tally.Count())
	}
	if got := scale(2, 3); got != 6 {
		t.Errorf("scale(2, 3) = %d, want 6", got)
	}
	if got := PlotArea(fakeRegion{}); got != 0 {
		t.Errorf("PlotArea(fakeRegion{}) = %d, want 0", got)
	}
}
