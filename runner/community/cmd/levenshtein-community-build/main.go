// Levenshtein-community-build compiles levenshtein-community-lint for the rule
// modules a configuration pins. The runner runs it in the build container;
// scripts/test-example-rules runs it on the host.
//
//	levenshtein-community-build -request request.json -out dir
//
// It writes dir/levenshtein-community-lint and dir/build.json, and exits 1
// with a message naming the module responsible when the build is refused.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wangjohn/levenshtein/runner/community/internal/build"
)

func main() {
	requestPath := flag.String("request", "", "read the build `request` (JSON) from this file")
	out := flag.String("out", "", "write the linter and build.json into this `directory`")
	flag.Parse()
	if err := run(*requestPath, *out); err != nil {
		fmt.Fprintf(os.Stderr, "building the community linter: %v\n", err)
		os.Exit(1)
	}
}

func run(requestPath, out string) error {
	if requestPath == "" || out == "" {
		return fmt.Errorf("-request and -out are required")
	}
	data, err := os.ReadFile(filepath.Clean(requestPath))
	if err != nil {
		return err
	}
	var req build.Request
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("reading %s: %w", requestPath, err)
	}

	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "levenshtein-community-build-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }() // Scratch space; the outputs are in out.

	result, err := build.Build(context.Background(), req, work, out)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "build.json"), append(encoded, '\n'), 0o644)
}
