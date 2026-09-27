package good

// Staticcheck lints this package twice, without and with its tests, and judges
// a //lint:ignore directive in each build. The findings below exist in one
// build only, and each directive must suppress its finding without the other
// build reporting it as matching nothing.

// Tally takes values everywhere in this file; builds_test.go adds a pointer
// receiver, so recvcheck reports the mix in the build with tests only.
//
//lint:ignore recvcheck the test helper resets a Tally in place
type Tally struct {
	count int
}

// Count returns how many values the tally has seen.
func (t Tally) Count() int {
	return t.count
}

// Scaled calls scale with a factor of 2 at every call site in this file, and
// builds_test.go passes 3, so unparam reports the factor without tests only.
func Scaled() []int {
	return []int{scale(1, 2), scale(2, 2), scale(3, 2), scale(4, 2)}
}

//lint:ignore unparam tests exercise other factors
func scale(value, factor int) int {
	return value * factor
}

// Region is a sum type; builds_test.go adds a fake variant that the switch in
// PlotArea does not list, which gochecksumtype must not report: a test's variant
// does not make the package's own switches incomplete.
//
//sumtype:decl
type Region interface {
	region()
}

// Plot is a Region.
type Plot struct {
	Side int
}

func (Plot) region() {}

// PlotArea lists every variant this package declares.
func PlotArea(region Region) int {
	switch region := region.(type) {
	case Plot:
		return region.Side * region.Side
	}
	return 0
}
