// Package assert is a local stand-in for testify's assert package, so the
// fixture can exercise testifylint without resolving testify over the network.
package assert

// TestingT is the subset of *testing.T that the assertions use.
type TestingT interface {
	Errorf(format string, args ...any)
}

func Equal(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	if expected != actual {
		t.Errorf("not equal: %v %v %v", expected, actual, msgAndArgs)
		return false
	}
	return true
}

func True(t TestingT, value bool, msgAndArgs ...any) bool {
	if !value {
		t.Errorf("not true: %v", msgAndArgs)
		return false
	}
	return true
}

func NoError(t TestingT, err error, msgAndArgs ...any) bool {
	if err != nil {
		t.Errorf("unexpected error: %v %v", err, msgAndArgs)
		return false
	}
	return true
}
