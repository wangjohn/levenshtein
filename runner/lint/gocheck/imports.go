package gocheck

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ImportTests says whether a rule also judges the imports of a package's
// _test.go files, including its external _test package.
type ImportTests string

const (
	TestsInclude ImportTests = "include"
	TestsExclude ImportTests = "exclude"
)

// ImportRule restricts what the packages it names may import. Packages are
// patterns relative to the checked directory, such as "./internal/store/...".
// Deny and Allow hold import path patterns, where an entry starting with "./"
// is relative to the checked directory's import path and "std" stands for the
// standard library. An import breaks the rule when a Deny entry matches it, or
// when Allow is set and no Allow entry does.
type ImportRule struct {
	Packages []string    `json:"packages"`
	Deny     []string    `json:"deny,omitempty"`
	Allow    []string    `json:"allow,omitempty"`
	Tests    ImportTests `json:"tests,omitempty"`
	Reason   string      `json:"reason"`
}

// ImportRules is the go-imports configuration, as levenshtein.json spells it.
type ImportRules struct {
	Rules []ImportRule `json:"rules"`
}

// stdPattern stands for every standard-library package, as it does for the go
// command.
const stdPattern = "std"

// ValidateImportRules rejects a configuration the check could not apply
// exactly as written. internal/verify/validation.go has a copy that checks
// levenshtein.json when a run is planned; both tests load
// runner/testdata/import-rules.json.
func ValidateImportRules(config ImportRules) error {
	if len(config.Rules) == 0 {
		return fmt.Errorf("go-imports needs a nonempty rules list")
	}
	for i, rule := range config.Rules {
		number := i + 1
		if len(rule.Packages) == 0 {
			return fmt.Errorf("go-imports rule %d needs a nonempty packages list", number)
		}
		for _, entry := range rule.Packages {
			if !localPattern(entry) {
				return fmt.Errorf("go-imports rule %d: package pattern %q must be \".\" or start with \"./\", like \"./internal/store/...\"", number, entry)
			}
		}
		if len(rule.Deny) == 0 && len(rule.Allow) == 0 {
			return fmt.Errorf("go-imports rule %d needs a deny or an allow list", number)
		}
		for _, entry := range slices.Concat(rule.Deny, rule.Allow) {
			if entry != stdPattern && !localPattern(entry) && !pathPattern(entry) {
				return fmt.Errorf("go-imports rule %d: import pattern %q must be an import path pattern such as \"example.com/app/internal/...\", a \"./\" pattern, or \"std\"", number, entry)
			}
		}
		if rule.Tests != "" && rule.Tests != TestsInclude && rule.Tests != TestsExclude {
			return fmt.Errorf("go-imports rule %d: tests %q must be %q or %q", number, rule.Tests, TestsInclude, TestsExclude)
		}
		if strings.TrimSpace(rule.Reason) == "" {
			return fmt.Errorf("go-imports rule %d needs a reason, which every finding it reports repeats", number)
		}
	}
	return nil
}

// localPattern is "." or a pattern relative to the checked directory.
func localPattern(entry string) bool {
	rest, ok := strings.CutPrefix(entry, "./")
	return entry == "." || (ok && pathPattern(rest))
}

// pathPattern is a slash-separated path with an optional "..." wildcard: no
// empty, "." or ".." element, and nothing a go.mod import path cannot hold.
func pathPattern(entry string) bool {
	if entry == "" || strings.ContainsAny(entry, " \t\r\n\"'`\\;:,\x00") {
		return false
	}
	for element := range strings.SplitSeq(entry, "/") {
		//lint:ignore LV1001 path elements are arbitrary text; these are the two relative spellings Go rejects.
		if element == "" || element == "." || element == ".." {
			return false
		}
	}
	return true
}

// importMatcher is one Deny or Allow entry, resolved against the module.
type importMatcher struct {
	text    string
	std     bool
	pattern pattern
}

// scope is what an import pattern needs to know about the checked directory:
// its own import path, for "./" patterns, and its module's path, so that
// "std" does not match a module whose path has no dot.
type scope struct {
	local  string
	module string
}

func (s scope) matcher(entry string) importMatcher {
	if entry == stdPattern {
		return importMatcher{text: entry, std: true}
	}
	resolved := entry
	if localPattern(entry) {
		resolved = s.local + strings.TrimPrefix(entry, ".")
	}
	return importMatcher{text: entry, pattern: compilePattern(resolved)}
}

func (s scope) standard(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	inModule := s.module != "" && (importPath == s.module || strings.HasPrefix(importPath, s.module+"/"))
	return importPath != "C" && !strings.Contains(first, ".") && !inModule
}

func (m importMatcher) match(s scope, importPath string) bool {
	if m.std {
		return s.standard(importPath)
	}
	return m.pattern.match(importPath)
}

// compiledRule is a rule with its patterns resolved against the module.
type compiledRule struct {
	number   int
	rule     ImportRule
	packages []pattern
	deny     []importMatcher
	allow    []importMatcher
}

// violation says why an import breaks the rule, or false when it does not.
// Deny wins over Allow, so an allow list can admit "std" and a deny list can
// still take one standard package back out.
func (r compiledRule) violation(s scope, importPath string) (string, bool) {
	for _, deny := range r.deny {
		if deny.match(s, importPath) {
			return fmt.Sprintf("denied by %q", deny.text), true
		}
	}
	if len(r.allow) == 0 {
		return "", false
	}
	for _, allow := range r.allow {
		if allow.match(s, importPath) {
			return "", false
		}
	}
	return "matches nothing in the allow list", true
}

// Imports checks every package under dir against the rules and reports each
// import that breaks one, at the import's own position. Files carrying a
// generated-code header are skipped. prefix is dir's path relative to the
// repository root, which the reported locations start with. A package pattern
// that matches no package is an error rather than a rule that silently checks
// nothing.
func Imports(ctx context.Context, dir, prefix string, config ImportRules, env []string) (Report, error) {
	if err := ValidateImportRules(config); err != nil {
		return Report{}, err
	}
	dir, err := resolvedDir(dir)
	if err != nil {
		return Report{}, err
	}
	// The go command turns cgo off when it finds no C compiler, and go list
	// then drops cgo files. Listing with cgo on judges them whatever the host
	// has, as the Dagger image, which has one, does; go list -find compiles
	// nothing, so it needs no compiler.
	packages, err := listPackages(ctx, dir, withEnv(env, "CGO_ENABLED=1"))
	if err != nil {
		return Report{}, err
	}

	names := make([]string, len(packages))
	var where scope
	for i, pkg := range packages {
		pkgDir, err := resolvedDir(pkg.Dir)
		if err != nil {
			return Report{}, err
		}
		packages[i].Dir = pkgDir
		rel, err := filepath.Rel(dir, pkgDir)
		if err != nil || !filepath.IsLocal(rel) && rel != "." {
			return Report{}, fmt.Errorf("go list reported package %s outside %s", pkg.ImportPath, dir)
		}
		names[i] = localName(filepath.ToSlash(rel))
		if where.local == "" {
			where.local = strings.TrimSuffix(pkg.ImportPath, strings.TrimPrefix(names[i], "."))
		}
		if where.module == "" && pkg.Module != nil {
			where.module = pkg.Module.Path
		}
	}

	rules := make([]compiledRule, len(config.Rules))
	for i, rule := range config.Rules {
		compiled := compiledRule{number: i + 1, rule: rule}
		for _, entry := range rule.Packages {
			matcher := compilePattern(entry)
			if !slices.ContainsFunc(names, matcher.match) {
				return Report{}, fmt.Errorf("go-imports rule %d: package pattern %q matches no package in %s", compiled.number, entry, prefix)
			}
			compiled.packages = append(compiled.packages, matcher)
		}
		for _, entry := range rule.Deny {
			compiled.deny = append(compiled.deny, where.matcher(entry))
		}
		for _, entry := range rule.Allow {
			compiled.allow = append(compiled.allow, where.matcher(entry))
		}
		rules[i] = compiled
	}

	files := importFiles{fset: token.NewFileSet(), parsed: map[string]*ast.File{}}
	var findings []Finding
	checked := map[string]bool{}
	for i, pkg := range packages {
		for _, rule := range rules {
			if !slices.ContainsFunc(rule.packages, func(p pattern) bool { return p.match(names[i]) }) {
				continue
			}
			checked[names[i]] = true
			for _, name := range pkg.files(rule.rule.Tests != TestsExclude) {
				found, err := files.check(filepath.Join(pkg.Dir, name), dir, prefix, names[i], rule, where)
				if err != nil {
					return Report{}, err
				}
				findings = append(findings, found...)
			}
		}
	}

	sortFindings(findings)
	note := fmt.Sprintf("%s checked %s in %s", count(len(rules), "rule"), count(len(checked), "package"), prefix)
	return Report{Findings: findings, Notes: []string{note}}, nil
}

// resolvedDir is dir as an absolute path without symlinks, so a package
// directory go list reports compares with the directory it was run in.
func resolvedDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// localName spells a directory relative to the checked one the way a package
// pattern does: "." for the directory itself, "./sub" below it.
func localName(rel string) string {
	if rel == "." {
		return "."
	}
	return "./" + rel
}

// importFiles parses each file once, however many rules name its package.
type importFiles struct {
	fset   *token.FileSet
	parsed map[string]*ast.File
}

func (f importFiles) check(file, dir, prefix, pkg string, rule compiledRule, where scope) ([]Finding, error) {
	parsed, ok := f.parsed[file]
	if !ok {
		var err error
		parsed, err = parser.ParseFile(f.fset, file, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("reading the imports of %s: %w", file, err)
		}
		f.parsed[file] = parsed
	}
	if ast.IsGenerated(parsed) {
		return nil, nil
	}

	rel, err := filepath.Rel(dir, file)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: import %s is not a string", file, spec.Path.Value)
		}
		why, broken := rule.violation(where, importPath)
		if !broken {
			continue
		}
		position := f.fset.Position(spec.Path.Pos())
		findings = append(findings, Finding{
			Code:     CodeImports,
			Message:  fmt.Sprintf("%s may not import %q (%s in the rule for %s): %s", pkg, importPath, why, strings.Join(rule.rule.Packages, ", "), rule.rule.Reason),
			Location: Location{File: path.Join(prefix, filepath.ToSlash(rel)), Line: position.Line, Column: position.Column},
		})
	}
	return findings, nil
}
