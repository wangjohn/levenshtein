package verify

import "testing"

func TestPreparationAndBuildHaveTheSamePlanningRules(t *testing.T) {
	for _, kind := range []string{"preparation", "build"} {
		for _, tc := range []struct {
			name   string
			change func(*Preparation)
		}{
			{"missing command", func(s *Preparation) { s.Command = nil }},
			{"missing inputs", func(s *Preparation) { s.Inputs = nil }},
			{"missing outputs", func(s *Preparation) { s.Outputs = nil }},
			{"timeout", func(s *Preparation) { s.Timeout = "-1s" }},
			{"environment", func(s *Preparation) { s.Env = map[string]string{"LEVENSHTEIN_RERUN_CHECKS": "true"} }},
			{"input escape", func(s *Preparation) { s.Inputs = []string{"../outside"} }},
			{"output escape", func(s *Preparation) { s.Outputs = []string{"../outside"} }},
			{"valid", func(*Preparation) {}},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				stage := Preparation{Command: []string{"true"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
				tc.change(&stage)
				var preparation, build string
				var preparations, builds map[string]Preparation
				if kind == "preparation" {
					preparation = "setup"
					preparations = map[string]Preparation{"setup": stage}
				} else {
					build = "setup"
					builds = map[string]Preparation{"setup": stage}
				}
				check := Check{
					Kind:        CheckCommand,
					Target:      "app",
					Environment: "host",
					Command:     &CommandCheck{Args: []string{"true"}, Preparation: preparation, Build: build},
				}
				cfg := Config{
					Version:      1,
					Targets:      map[string]Target{"app": {Dir: ".", Inputs: []string{"."}}},
					Environments: map[string]Environment{"host": {Executor: ExecutorNative}},
					Runs:         map[string]Run{"branch": {Checks: []string{"test"}}},
					Checks:       map[string]Check{"test": check},
					Preparations: preparations,
					Builds:       builds,
				}
				_, err := cfg.Plan(t.TempDir(), "branch")
				if (err == nil) != (tc.name == "valid") {
					t.Fatalf("plan error: %v", err)
				}
			})
		}
	}
}
