package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/wangjohn/levenshtein/internal/verify"
)

func TestParseArgs(t *testing.T) {
	defaults := options{source: "/default/source", shared: "/default/shared"}
	for _, tt := range []struct {
		name    string
		args    []string
		want    options
		wantErr string
	}{
		{name: "defaults", want: options{format: verify.FormatJSON, source: defaults.source, shared: defaults.shared, name: "branch"}},
		{name: "flags after run", args: []string{"pre-merge", "--dry-run", "--source", "/app", "--shared", "/tools"}, want: options{format: verify.FormatJSON, source: "/app", shared: "/tools", name: "pre-merge", dry: true}},
		{name: "flags before run", args: []string{"--source=/app", "--shared=/tools", "--dry-run", "pre-merge"}, want: options{format: verify.FormatJSON, source: "/app", shared: "/tools", name: "pre-merge", dry: true}},
		{name: "override launcher defaults", args: []string{"--source", "/launcher", "--shared", "/launcher", "custom-run", "--source=/app", "--shared=/tools"}, want: options{format: verify.FormatJSON, source: "/app", shared: "/tools", name: "custom-run"}},
		{name: "explicit false", args: []string{"--dry-run", "branch", "--dry-run=false"}, want: options{format: verify.FormatJSON, source: defaults.source, shared: defaults.shared, name: "branch"}},
		{name: "terminator", args: []string{"--", "--custom-run"}, want: options{format: verify.FormatJSON, source: defaults.source, shared: defaults.shared, name: "--custom-run"}},
		{name: "jobs", args: []string{"branch", "--jobs", "8"}, want: options{format: verify.FormatJSON, source: defaults.source, shared: defaults.shared, name: "branch", jobs: 8}},
		{name: "negative jobs", args: []string{"--jobs=-1"}, wantErr: "--jobs cannot be negative"},
		{name: "multiple runs", args: []string{"branch", "main"}, wantErr: "expected at most one run"},
		{name: "unknown flag", args: []string{"branch", "--unknown"}, wantErr: "unknown flag"},
		{name: "missing source", args: []string{"branch", "--source"}, wantErr: "needs an argument"},
		{name: "missing shared", args: []string{"branch", "--shared"}, wantErr: "needs an argument"},
		{name: "invalid boolean", args: []string{"--dry-run=maybe"}, wantErr: "invalid argument"},
		{name: "empty source", args: []string{"--source="}, wantErr: "source directory cannot be empty"},
		{name: "text format", args: []string{"branch", "--format", "text"}, want: options{format: verify.FormatText, source: defaults.source, shared: defaults.shared, name: "branch"}},
		{name: "github format with prefix", args: []string{"--format=github", "--path-prefix", "./app/", "pre-merge"}, want: options{format: verify.FormatGitHub, pathPrefix: "app", source: defaults.source, shared: defaults.shared, name: "pre-merge"}},
		{name: "unknown format", args: []string{"--format", "xml"}, wantErr: "--format must be one of json, text, github, sarif"},
		{name: "render", args: []string{"--render", "report.json", "--format", "sarif"}, want: options{format: verify.FormatSARIF, render: "report.json", source: defaults.source, shared: defaults.shared, name: "branch"}},
		{name: "render with a run", args: []string{"--render", "-", "main"}, wantErr: "--render writes a saved report and runs nothing"},
		{name: "dry run with a format", args: []string{"--dry-run", "--format", "text"}, wantErr: "--dry-run prints the plan as JSON"},
		{name: "prefix on json", args: []string{"--path-prefix", "app"}, wantErr: "--path-prefix applies to text, github and sarif"},
		{name: "prefix outside the checkout", args: []string{"--format", "text", "--path-prefix", "../app"}, wantErr: "must be a relative directory"},
		{name: "absolute prefix", args: []string{"--format", "text", "--path-prefix", "/app"}, wantErr: "must be a relative directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := parseArgs(tt.args, defaults, &output)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
			if output.Len() != 0 {
				t.Fatalf("parser polluted report output: %s", &output)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"branch", "-h"}} {
		var output bytes.Buffer
		_, err := parseArgs(args, options{}, &output)
		if !errors.Is(err, pflag.ErrHelp) {
			t.Fatalf("help error = %v", err)
		}
		for _, text := range []string{"Usage: verify", "--source", "--shared", "--dry-run", "--help", "--cache-dir", "--jobs", "--format", "--render", "--path-prefix"} {
			if !strings.Contains(output.String(), text) {
				t.Errorf("help missing %q: %s", text, &output)
			}
		}
	}
}

func TestCacheDirFlag(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"branch", "--cache-dir", "/cache"},
		{"--cache-dir=/cache", "branch"},
	} {
		var output bytes.Buffer
		want := "/default/cache"
		if len(args) > 0 {
			want = "/cache"
		}
		opts, err := parseArgs(args, options{source: "/app", cacheDir: "/default/cache"}, &output)
		if err != nil || opts.cacheDir != want {
			t.Fatalf("cache directory for %q = %q, %v", args, opts.cacheDir, err)
		}
	}
	if _, err := parseArgs([]string{"--cache-dir"}, options{source: "/app"}, &bytes.Buffer{}); err == nil {
		t.Fatal("missing cache directory accepted")
	}
}
