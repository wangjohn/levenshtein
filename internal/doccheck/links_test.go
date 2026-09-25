package doccheck

import (
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// repoRoot is the repository root, relative to this package's directory.
const repoRoot = "../.."

// pendingTargets are repository files that documentation already links to but
// that a change still in review adds. A link to one passes while the file is
// missing; delete the entry once the file exists.
var pendingTargets = map[string]bool{
	"docs/check-kinds.md": true,
}

// skippedDirs hold Markdown that is not documentation: fixtures, vendored or
// generated trees, and build output.
var skippedDirs = map[string]bool{
	".git":         true,
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
	"dist":         true,
}

var (
	inlineLink    = regexp.MustCompile(`\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	referenceLink = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s+"[^"]*")?\s*$`)
	codeSpan      = regexp.MustCompile("`+[^`]*`+")
	htmlAnchor    = regexp.MustCompile(`<a\s+(?:id|name)="([^"]+)"`)
	scheme        = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	headingLink   = regexp.MustCompile(`!?\[([^\]]*)\](?:\([^)]*\)|\[[^\]]*\])?`)
	atxHeading    = regexp.MustCompile(`^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$`)
)

// document is one Markdown file: its links, in order, and the anchors its
// headings and HTML anchors define.
type document struct {
	links   []link
	anchors map[string]bool
}

type link struct {
	line   int
	target string
}

// TestRelativeLinksResolve checks every relative link in the repository's
// Markdown: the file or directory it names must exist, and a #fragment must
// match a heading or HTML anchor in the Markdown file it points at, using
// GitHub's anchor rules. External links are not fetched.
func TestRelativeLinksResolve(t *testing.T) {
	docs := map[string]document{}
	err := filepath.WalkDir(repoRoot, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() && p != repoRoot && (skippedDirs[name] || (strings.HasPrefix(name, ".") && name != ".github")) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		docs[filepath.ToSlash(rel)] = parse(string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 10 {
		t.Fatalf("found only %d Markdown files under %s; is the walk rooted correctly?", len(docs), repoRoot)
	}

	for _, file := range slices.Sorted(maps.Keys(docs)) {
		for _, l := range docs[file].links {
			if problem := resolve(docs, file, l.target); problem != "" {
				t.Errorf("%s:%d: link %q: %s", file, l.line, l.target, problem)
			}
		}
	}
}

// resolve returns why target, linked from file, does not resolve, or "".
func resolve(docs map[string]document, file, target string) string {
	if scheme.MatchString(target) || strings.HasPrefix(target, "//") {
		return ""
	}
	target, fragment, _ := strings.Cut(target, "#")
	if query := strings.IndexByte(target, '?'); query >= 0 {
		target = target[:query]
	}

	resolved := file
	if target != "" {
		decoded, err := url.PathUnescape(target)
		if err != nil {
			return fmt.Sprintf("bad escape: %v", err)
		}
		if strings.HasPrefix(decoded, "/") {
			resolved = path.Clean(strings.TrimPrefix(decoded, "/"))
		} else {
			resolved = path.Join(path.Dir(file), decoded)
		}
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return "points outside the repository"
		}
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(resolved))); err != nil {
			if pendingTargets[resolved] {
				return ""
			}
			return fmt.Sprintf("%s does not exist", resolved)
		}
	}

	if fragment == "" || !strings.HasSuffix(resolved, ".md") {
		return ""
	}
	doc, ok := docs[resolved]
	if !ok {
		return ""
	}
	if !doc.anchors[fragment] {
		return fmt.Sprintf("%s has no heading with anchor #%s", resolved, fragment)
	}
	return ""
}

// parse collects the links and anchors of one Markdown file, skipping fenced
// code blocks and inline code.
func parse(text string) document {
	doc := document{anchors: map[string]bool{}}
	seen := map[string]int{}
	fence := ""
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}

		for _, match := range htmlAnchor.FindAllStringSubmatch(line, -1) {
			doc.anchors[match[1]] = true
		}
		if match := atxHeading.FindStringSubmatch(line); match != nil {
			base := slug(match[2])
			anchor := base
			if n := seen[base]; n > 0 {
				anchor = fmt.Sprintf("%s-%d", base, n)
			}
			seen[base]++
			doc.anchors[anchor] = true
		}

		if match := referenceLink.FindStringSubmatch(line); match != nil {
			doc.links = append(doc.links, link{line: i + 1, target: match[1]})
			continue
		}
		for _, match := range inlineLink.FindAllStringSubmatch(codeSpan.ReplaceAllString(line, ""), -1) {
			doc.links = append(doc.links, link{line: i + 1, target: match[1]})
		}
	}
	return doc
}

// slug is GitHub's anchor for a heading: the rendered text lowercased, with
// every character that is not a letter, digit, space, hyphen, or underscore
// dropped, and each space turned into a hyphen.
func slug(heading string) string {
	heading = headingLink.ReplaceAllString(heading, "$1")
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestSlugFollowsGitHub(t *testing.T) {
	cases := map[string]string{
		"Levenshtein's own CI":                  "levenshteins-own-ci",
		"Opt-in complexity: gocognit":           "opt-in-complexity-gocognit",
		"The `./verify` launcher":               "the-verify-launcher",
		"[0.1.0] - 2026-09-22":                  "010---2026-09-22",
		"Typed choices: LV1001":                 "typed-choices-lv1001",
		"`rule_modules` and [links](x.md) here": "rule_modules-and-links-here",
	}
	for heading, want := range cases {
		if got := slug(heading); got != want {
			t.Errorf("slug(%q) = %q, want %q", heading, got, want)
		}
	}
}

func TestResolveReportsMissingFilesAndAnchors(t *testing.T) {
	docs := map[string]document{"docs/setup.md": parse("# Setup\n\n## Prerequisites\n")}

	cases := map[string]bool{
		"docs/setup.md#prerequisites": true,
		"docs/setup.md#nothing-here":  false,
		"docs/no-such-file.md":        false,
		"../outside.md":               false,
		"https://example.com/x#y":     true,
		"LICENSE":                     true,
	}
	for target, ok := range cases {
		if problem := resolve(docs, "README.md", target); (problem == "") != ok {
			t.Errorf("resolve(%q) = %q, want ok=%v", target, problem, ok)
		}
	}
}

func TestParseSkipsCodeAndNumbersDuplicateHeadings(t *testing.T) {
	doc := parse("# Title\n\n```md\n[in code](missing.md)\n## Hidden\n```\n\nSee `[span](missing.md)` and [real](other.md#x).\n\n## Title\n\n[ref]: ref.md\n")

	if want := []link{{line: 8, target: "other.md#x"}, {line: 12, target: "ref.md"}}; !slices.Equal(doc.links, want) {
		t.Errorf("links = %+v, want %+v", doc.links, want)
	}
	if !doc.anchors["title"] || !doc.anchors["title-1"] || doc.anchors["hidden"] {
		t.Errorf("anchors = %v", doc.anchors)
	}
}
