package accepted

import "testing"

func TestClamp(t *testing.T) {
	for _, c := range []struct {
		n    int
		want int
	}{{-1, 0}, {0, 0}, {5, 5}, {10, 10}, {11, 10}} {
		if got := Clamp(c.n, 0, 10); got != c.want {
			t.Errorf("Clamp(%d, 0, 10) = %d, want %d", c.n, got, c.want)
		}
	}
}
