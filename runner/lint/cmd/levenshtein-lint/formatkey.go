package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// LV1005 also checks the Go files a build leaves out, for another platform or
// behind a build tag, which it reads from the package directory. Staticcheck
// keys a package's cached results on the files the build compiles, so a cached
// result would keep reporting an excluded file after it is formatted, or miss
// one that is not. Staticcheck's key also includes the GODEBUG environment
// variable (lintcmd/runner/runner.go in honnef.co/go/tools v0.8.1), so the
// linter adds a digest of every excluded file under the patterns to it. The
// runtime and the go command ignore a setting they do not know. A module with
// no excluded files keeps GODEBUG, and so its cache, as it was; one that has
// them re-lints every package when one of them changes.

// ignoredSetting is the GODEBUG setting that carries the digest.
const ignoredSetting = "levenshteinignored"

// keyIgnoredFiles adds the digest of the excluded files under patterns to
// GODEBUG before Staticcheck computes its cache keys. When go list fails, the
// run fails with Staticcheck's own report of the same error, so the key is
// left alone.
func keyIgnoredFiles(patterns []string) {
	digest, err := ignoredDigest(patterns)
	if err != nil || digest == "" {
		return
	}
	setting := ignoredSetting + "=" + digest
	if current := os.Getenv("GODEBUG"); current != "" {
		setting = current + "," + setting
	}
	if err := os.Setenv("GODEBUG", setting); err != nil {
		panic(err)
	}
}

// ignoredDigest hashes the path and contents of every Go file that go list
// reports as excluded from the packages matching patterns, under the default
// build context LV1005 uses. It returns "" when there are none.
func ignoredDigest(patterns []string) (string, error) {
	listing, err := exec.CommandContext(context.Background(), "go", append([]string{"list", "-e", "-json=Dir,IgnoredGoFiles"}, patterns...)...).Output()
	if err != nil {
		return "", fmt.Errorf("listing excluded files: %w", err)
	}

	hash := sha256.New()
	found := false
	decoder := json.NewDecoder(bytes.NewReader(listing))
	for {
		var pkg struct {
			Dir            string   `json:"Dir"`
			IgnoredGoFiles []string `json:"IgnoredGoFiles"`
		}
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return "", fmt.Errorf("reading go list output: %w", err)
		}
		for _, base := range pkg.IgnoredGoFiles {
			name := filepath.Join(pkg.Dir, base)
			source, err := os.ReadFile(name)
			if err != nil {
				return "", err
			}
			hash.Write(fmt.Appendf(nil, "%s %d\n", name, len(source)))
			hash.Write(source)
			found = true
		}
	}
	if !found {
		return "", nil
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
