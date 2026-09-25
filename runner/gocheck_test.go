package main

import (
	"testing"
)

func TestGocheckRefusesAModuleOutsideTheSource(t *testing.T) {
	for _, module := range []string{"../app", "/app", "a/../b", `a\b`} {
		if validModule(module) == nil {
			t.Errorf("%q must be refused", module)
		}
	}
	if err := validModule("services/api"); err != nil {
		t.Fatal(err)
	}
}
