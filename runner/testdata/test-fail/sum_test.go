package sum

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 2); got != 5 {
		t.Errorf("Add(2, 2) = %d, want 5", got)
	}
}

// TestPasses keeps a passing test beside the failing one, whose output the
// finding leaves out.
func TestPasses(t *testing.T) {}
