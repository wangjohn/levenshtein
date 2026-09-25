package buildparams

import "testing"

// A helper in a test file is judged by the build with tests.
func check(t *testing.T, got, unused int) { // want `unused is unused`
	t.Helper()
	if got == 0 {
		t.Error("zero")
	}
}

func TestParams(t *testing.T) {
	check(t, scale(2, 3), 0)
	check(t, shift(4, 5), 0)
}
