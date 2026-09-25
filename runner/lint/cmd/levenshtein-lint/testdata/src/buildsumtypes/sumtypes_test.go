package buildsumtypes

import "testing"

type fakeRegion struct{}

func (fakeRegion) region() {}

// A switch in a test file is judged by the build with tests.
func name(region Region) string {
	switch region.(type) { // want `missing cases for Plot`
	case fakeRegion:
		return "fake"
	}
	return ""
}

func TestArea(t *testing.T) {
	if Area(fakeRegion{}) != 0 || name(fakeRegion{}) != "fake" {
		t.Error("wrong")
	}
}
