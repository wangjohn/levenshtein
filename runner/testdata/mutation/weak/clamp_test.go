package weak

import "testing"

// TestClamp only checks a value inside the range, so moving a boundary goes
// unnoticed.
func TestClamp(t *testing.T) {
	if got := Clamp(5, 0, 10); got != 5 {
		t.Fatalf("Clamp(5, 0, 10) = %d, want 5", got)
	}
}
