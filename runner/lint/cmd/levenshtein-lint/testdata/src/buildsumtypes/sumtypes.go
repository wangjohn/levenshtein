package buildsumtypes

// The build with tests sees the fake variant in sumtypes_test.go, which Area
// does not list; the build without tests decides this file's switches.

//sumtype:decl
type Region interface {
	region()
}

type Plot struct{}

func (Plot) region() {}

func Area(region Region) int {
	switch region.(type) {
	case Plot:
		return 1
	}
	return 0
}
