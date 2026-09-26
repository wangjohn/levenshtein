package consumer

import "testing"

func TestDoubled(t *testing.T) {
	if got := Doubled(); got != 84 {
		t.Fatalf("Doubled() = %d, want 84", got)
	}
}
