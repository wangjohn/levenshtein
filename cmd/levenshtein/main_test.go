package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCommandPlanningHelpAndErrorsNeedNoExecutor(t *testing.T) {
	t.Setenv("LEVENSHTEIN_SHARED_ROOT", "")
	t.Setenv("PATH", "")
	source := t.TempDir()
	for _, tc := range []struct {
		name    string
		args    []string
		code    int
		message string
	}{
		{"plan", []string{"--source", source, "branch", "--dry-run"}, 0, ""},
		{"help", []string{"--help"}, 0, "Usage: verify"},
		{"unknown flag", []string{"--unknown"}, 2, "unknown flag"},
		{"missing run", []string{"--source", source, "unknown", "--dry-run"}, 2, "missing or empty"},
		{"missing shared", []string{"--source", source}, 2, "set --shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			code, err := runCommand(context.Background(), tc.args, &output)
			if code != tc.code {
				t.Fatalf("code=%d error=%v", code, err)
			}
			if code == 2 {
				if err == nil || !strings.Contains(err.Error(), tc.message) || output.Len() != 0 {
					t.Fatalf("error=%v output=%s", err, &output)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "plan" {
				if !json.Valid(output.Bytes()) {
					t.Fatalf("invalid plan JSON: %s", &output)
				}
			} else if !strings.Contains(output.String(), tc.message) {
				t.Fatalf("help: %s", &output)
			}
		})
	}
	if code, err := runCommand(context.Background(), []string{"--source", source, "--dry-run"}, failingOutput{}); code != 2 || err == nil {
		t.Fatalf("lost output error: code=%d error=%v", code, err)
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }
