package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestParseArgs(t *testing.T) {
	defaults := options{source: "/default/source", shared: "/default/shared"}
	for _, tt := range []struct {
		name    string
		args    []string
		want    options
		wantErr string
	}{
		{name: "defaults", want: options{source: defaults.source, shared: defaults.shared, name: "branch"}},
		{name: "flags after run", args: []string{"pre-merge", "--dry-run", "--source", "/app", "--shared", "/tools"}, want: options{source: "/app", shared: "/tools", name: "pre-merge", dry: true}},
		{name: "flags before run", args: []string{"--source=/app", "--shared=/tools", "--dry-run", "pre-merge"}, want: options{source: "/app", shared: "/tools", name: "pre-merge", dry: true}},
		{name: "override launcher defaults", args: []string{"--source", "/launcher", "--shared", "/launcher", "custom-run", "--source=/app", "--shared=/tools"}, want: options{source: "/app", shared: "/tools", name: "custom-run"}},
		{name: "explicit false", args: []string{"--dry-run", "branch", "--dry-run=false"}, want: options{source: defaults.source, shared: defaults.shared, name: "branch"}},
		{name: "terminator", args: []string{"--", "--custom-run"}, want: options{source: defaults.source, shared: defaults.shared, name: "--custom-run"}},
		{name: "multiple runs", args: []string{"branch", "main"}, wantErr: "expected at most one run"},
		{name: "unknown flag", args: []string{"branch", "--unknown"}, wantErr: "unknown flag"},
		{name: "missing source", args: []string{"branch", "--source"}, wantErr: "needs an argument"},
		{name: "missing shared", args: []string{"branch", "--shared"}, wantErr: "needs an argument"},
		{name: "invalid boolean", args: []string{"--dry-run=maybe"}, wantErr: "invalid argument"},
		{name: "empty source", args: []string{"--source="}, wantErr: "source directory cannot be empty"},
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
		for _, text := range []string{"Usage: verify", "--source", "--shared", "--dry-run", "--help"} {
			if !strings.Contains(output.String(), text) {
				t.Errorf("help missing %q: %s", text, &output)
			}
		}
	}
}
