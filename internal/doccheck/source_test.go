package doccheck

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// docURL is a link to a file on the default branch, the form finding URLs
	// and hints take. Consumers pinned to an older release still follow these
	// links to main, so a moved heading has to leave its anchor behind.
	docURL = regexp.MustCompile(`https://github\.com/wangjohn/levenshtein/blob/main/([A-Za-z0-9_./-]+)(?:#([A-Za-z0-9_-]+))?`)
	// docPath is a bare docs/ Markdown path, as comments and error messages
	// name them.
	docPath = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])(docs/[A-Za-z0-9_./-]*\.md)(?:#([A-Za-z0-9_-]+))?`)
)

// sourceExtensions are the non-Markdown files that point readers at the docs.
// Extensionless scripts are recognized by their shebang.
var sourceExtensions = map[string]bool{".go": true, ".yml": true, ".yaml": true, ".json": true, ".sh": true}

// A reference is one documentation link found in a source file.
type reference struct {
	file   string
	line   int
	target string
}

// TestSourceDocLinksResolve checks the documentation links in code,
// configuration, and scripts: every github.com/wangjohn/levenshtein/blob/main
// URL, and every bare docs/*.md path outside tests and testdata, must name a
// file that exists, and a #fragment must match a heading or HTML anchor in it.
func TestSourceDocLinksResolve(t *testing.T) {
	docs := loadDocs(t)
	refs := sourceReferences(t)
	if len(refs) < 20 {
		t.Fatalf("found only %d documentation references in source files; is the walk rooted correctly?", len(refs))
	}

	for _, ref := range refs {
		if problem := resolve(docs, "", "/"+ref.target); problem != "" {
			t.Errorf("%s:%d: %q: %s", ref.file, ref.line, ref.target, problem)
		}
	}
}

// releasedDocLinks are the documentation links that released versions put in
// findings, hints, and error messages. Those binaries keep printing them, and
// every one resolves against main, so a heading they name has to stay, if only
// as a pointer, after the current code stops linking to it.
var releasedDocLinks = []string{
	// v0.2.0: finding URLs and LV hints.
	"docs/checks.md#go-lint-rules",
	"docs/checks.md#typed-choices-lv1001",
	"docs/checks.md#construct-value-records-together-lv1002",
	"docs/checks.md#one-field-per-line-lv1003",
	"docs/checks.md#a-blank-line-between-declarations-lv1004",
	"docs/checks.md#formatted-files-lv1005",
	"docs/checks.md#tests-that-can-fail-lv1006",
	"docs/community-rules.md#ignore-directives",
	// v0.2.0: error messages.
	"docs/checks.md#generated-code",
	"docs/community-rules.md#the-contract",
	"docs/community-rules.md#upgrades-and-withdrawals",
	"docs/configuration.md#baseline",
}

func TestReleasedDocLinksResolve(t *testing.T) {
	docs := loadDocs(t)

	for _, target := range releasedDocLinks {
		if problem := resolve(docs, "", "/"+target); problem != "" {
			t.Errorf("%q, linked from a released version: %s", target, problem)
		}
	}
}

// sourceReferences collects the documentation links in the repository's
// source files. Links by URL are collected everywhere, testdata included,
// since fixtures such as runner/testdata/core-urls.json pin real URLs. Bare
// paths are skipped in tests and testdata, where they are often made up.
func sourceReferences(t *testing.T) []reference {
	t.Helper()
	var refs []reference
	err := filepath.WalkDir(repoRoot, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if p != repoRoot && name != "testdata" && (skippedDirs[name] || (strings.HasPrefix(name, ".") && name != ".github")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		ext := filepath.Ext(name)
		script := ext == "" && bytes.HasPrefix(data, []byte("#!"))
		if !sourceExtensions[ext] && !script {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fixture := strings.HasSuffix(name, "_test.go") || strings.Contains("/"+rel, "/testdata/")

		for i, line := range strings.Split(string(data), "\n") {
			for _, match := range docURL.FindAllStringSubmatch(line, -1) {
				refs = append(refs, reference{file: rel, line: i + 1, target: withFragment(strings.TrimRight(match[1], "."), match[2])})
			}
			if fixture {
				continue
			}
			for _, match := range docPath.FindAllStringSubmatch(line, -1) {
				refs = append(refs, reference{file: rel, line: i + 1, target: withFragment(match[1], match[2])})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func withFragment(target, fragment string) string {
	if fragment == "" {
		return target
	}
	return target + "#" + fragment
}

func TestSourceReferencePatterns(t *testing.T) {
	cases := map[string][]string{
		`return "https://github.com/wangjohn/levenshtein/blob/main/docs/rules.md#rules-on-by-default"`: {"docs/rules.md", "rules-on-by-default"},
		`// See docs/community-rules.md.`:                                    {"docs/community-rules.md", ""},
		`fmt.Errorf("see docs/configuration.md#baseline")`:                   {"docs/configuration.md", "baseline"},
		`# (docs/check-kinds-guide.md#generated-code).`:                      {"docs/check-kinds-guide.md", "generated-code"},
		`see https://github.com/wangjohn/levenshtein/blob/main/SECURITY.md.`: {"SECURITY.md", ""},
	}
	for line, want := range cases {
		match := docPath.FindStringSubmatch(line)
		if strings.Contains(line, "https://") {
			match = docURL.FindStringSubmatch(line)
			match[1] = strings.TrimRight(match[1], ".")
		}
		if match == nil || match[1] != want[0] || match[2] != want[1] {
			t.Errorf("%s: matched %q, want %q", line, match, want)
		}
	}

	if match := docPath.FindStringSubmatch("see mydocs/other.md"); match != nil {
		t.Errorf("docPath matched a path that does not start at docs/: %q", match)
	}
}
