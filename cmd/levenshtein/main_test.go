package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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
			code, err := runCommand(context.Background(), tc.args, console{out: &output, err: io.Discard})
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
	if code, err := runCommand(context.Background(), []string{"--source", source, "--dry-run"}, console{out: failingOutput{}, err: io.Discard}); code != 2 || err == nil {
		t.Fatalf("lost output error: code=%d error=%v", code, err)
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

// A cache directory inside the shared checkout is rejected however the
// checkout is spelled: through a symlink, and before the directory exists.
func TestCacheDirInsideSymlinkedSharedCheckoutIsRejected(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	link := filepath.Join(base, "link")
	source := filepath.Join(base, "src")
	for _, dir := range []string{target, source} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	for _, shared := range []string{target, link} {
		for _, cache := range []string{filepath.Join(target, "cache", "deep"), filepath.Join(link, "cache", "deep")} {
			code, err := runCommand(context.Background(), []string{"branch", "--source", source, "--shared", shared, "--cache-dir", cache}, console{out: io.Discard, err: io.Discard})
			if code != 2 || err == nil || !strings.Contains(err.Error(), "cache directory must be outside") {
				t.Fatalf("shared %s cache %s: code %d err %v", shared, cache, code, err)
			}
		}
	}

	code, err := runCommand(context.Background(), []string{"branch", "--source", source, "--shared", filepath.Join(base, "absent")}, console{out: io.Discard, err: io.Discard})
	if code != 2 || err == nil || !strings.Contains(err.Error(), "--shared") {
		t.Fatalf("missing shared checkout: code %d err %v", code, err)
	}
}
