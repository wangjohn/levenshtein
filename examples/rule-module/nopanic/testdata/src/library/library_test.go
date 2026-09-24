package library

import "testing"

func TestParse(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Parse accepted empty input")
		}
	}()
	panic("tests may panic")
}
