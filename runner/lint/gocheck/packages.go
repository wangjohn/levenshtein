package gocheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// listedPackage is the part of a go list -json record the checks read. The
// tags are go list's field names.
type listedPackage struct {
	Dir          string        `json:"Dir"`
	ImportPath   string        `json:"ImportPath"`
	Name         string        `json:"Name"`
	GoFiles      []string      `json:"GoFiles"`
	CgoFiles     []string      `json:"CgoFiles"`
	TestGoFiles  []string      `json:"TestGoFiles"`
	XTestGoFiles []string      `json:"XTestGoFiles"`
	Module       *listedModule `json:"Module"`
}

type listedModule struct {
	Path string `json:"Path"`
}

// files are the package's Go files for this platform, with or without its
// tests, as names relative to its directory.
func (p listedPackage) files(tests bool) []string {
	files := slices.Concat(p.GoFiles, p.CgoFiles)
	if tests {
		files = slices.Concat(files, p.TestGoFiles, p.XTestGoFiles)
	}
	return files
}

// listPackages lists the packages under dir with go list -find, which reads
// each package's files and import declarations without resolving imports, so
// it needs no module downloads and no type checking. Directories go list
// skips, such as testdata and vendor, and nested modules are not listed. A
// module without packages is an error, so a check cannot pass by judging
// nothing.
func listPackages(ctx context.Context, dir string, env []string) ([]listedPackage, error) {
	listed, err := command(ctx, dir, env, "go", "list", "-find", "-json=Dir,ImportPath,Name,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles,Module", "./...")
	if err != nil {
		return nil, err
	}
	if listed.ExitCode != 0 {
		return nil, fmt.Errorf("go list failed: %s", listed.output())
	}

	var packages []listedPackage
	decoder := json.NewDecoder(strings.NewReader(listed.Stdout))
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("go list printed something other than packages: %w", err)
		}
		packages = append(packages, pkg)
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("the module contains no Go packages; refusing an empty pass")
	}
	return packages, nil
}

// pattern matches package or import paths the way the go command's patterns
// do: "..." matches any string, including one with slashes, and a pattern that
// ends in "/..." also matches the path before it, so "x/..." matches x.
type pattern struct {
	text string
	re   *regexp.Regexp
}

func compilePattern(text string) pattern {
	expr := strings.ReplaceAll(regexp.QuoteMeta(text), `\.\.\.`, `.*`)
	if trimmed, ok := strings.CutSuffix(expr, `/.*`); ok {
		expr = trimmed + `(/.*)?`
	}
	return pattern{text: text, re: regexp.MustCompile(`^` + expr + `$`)}
}

func (p pattern) match(name string) bool {
	return p.re.MatchString(name)
}
