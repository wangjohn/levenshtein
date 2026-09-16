package main

import "testing"

func TestCustomRun(t *testing.T) {
	cfg, err := parseConfig(`{"modules":["."],"runs":{"cleanup":["go-lint"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := cfg.selectChecks("cleanup")
	if err != nil || len(checks) != 1 || checks[0] != "go-lint" {
		t.Fatalf("custom run: %v, %v", checks, err)
	}
}

func TestRejectIncompleteOrUnsafeConfiguration(t *testing.T) {
	for _, content := range []string{
		`{"modules":[],"runs":{"branch":["go-lint"]}}`,
		`{"modules":[".."],"runs":{"branch":["go-lint"]}}`,
		`{"modules":["/tmp"],"runs":{"branch":["go-lint"]}}`,
		`{"modules":["a/../b"],"runs":{"branch":["go-lint"]}}`,
		`{"modules":[".","."],"runs":{"branch":["go-lint"]}}`,
		`{"modules":["."],"runs":{"branch":[]}}`,
		`{"modules":["."],"runs":{"another":["go-lint"]}}`,
		`{"modules":["."],"runs":{"branch":["typo"]}}`,
		`{"modules":["."],"runs":{"branch":["go-lint","go-lint"]}}`,
		`{"modules":["."],"run":{"branch":["go-lint"]}}`,
		`{"modules":["."],"runs":{"branch":["go-lint"]}} {}`,
	} {
		t.Run(content, func(t *testing.T) {
			cfg, err := parseConfig(content)
			if err == nil {
				_, err = cfg.selectChecks("branch")
			}
			if err == nil {
				t.Fatal("invalid configuration would have been accepted")
			}
		})
	}
}
