package bad

// unparam: no caller needs the label, so the parameter only misleads.
func double(value int, label string) int {
	return value * 2
}

// Double calls double so it is not dead code.
func Double(value int, label string) int {
	return double(value, label)
}
