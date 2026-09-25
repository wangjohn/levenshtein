package buildparams

// The build without tests and the build with tests both check this file. Only
// the build without tests sees scale always receive 2, since params_test.go
// passes 3; the build with tests leaves the line and its directive to it.

func Scaled() []int {
	return []int{scale(1, 2), scale(2, 2), scale(3, 2), scale(4, 2)}
}

//lint:ignore unparam tests exercise other factors
func scale(value, factor int) int { // want `factor always receives 2|is judged in the build without tests`
	return value * factor
}

func Shifted() []int {
	return []int{shift(1, 5), shift(2, 5), shift(3, 5)}
}

// The build with tests sees shift always receive 5 at the four call sites
// unparam requires, but its findings in this file are the build without
// tests' to make, which sees three.
func shift(value, by int) int {
	return value + by
}
