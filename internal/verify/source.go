package verify

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
)

// literalPath is a declared path Dagger can use verbatim: no pattern syntax and
// no negation, so configuration cannot widen or invert an import rule.
func literalPath(path string) bool {
	return relative(path) && !strings.ContainsAny(path, "*?[]{}!\n\r")
}

func daggerIncludes(inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("Dagger checks require explicit input paths")
	}

	var includes []string
	for _, input := range inputs {
		if !literalPath(input) {
			return nil, fmt.Errorf("Dagger input %q must be a literal repository-relative path without pattern characters", input)
		}
		if input == "." {
			includes = append(includes, "**")
		} else {
			includes = append(includes, subtreePatterns(filepath.ToSlash(input))...)
		}
	}
	return includes, nil
}

func privateSourcePath(path string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		//lint:ignore LV1001 Filesystem components are arbitrary paths, not an enum.
		if part == ".git" || part == ".env" || (strings.HasPrefix(part, ".env.") && part != ".env.example") {
			return true
		}
	}
	return false
}

// daggerExcludes keeps private files out of every import and adds the target's
// own excluded paths, so a declared input can cover a directory without carrying
// its dependency or build output into the container.
func daggerExcludes(excludes []string) []string {
	out := []string{"**/.env", "**/.env.*", "!**/.env.example", "**/.git"}
	for _, path := range excludes {
		out = append(out, subtreePatterns(filepath.ToSlash(path))...)
	}
	return out
}

// subtreePatterns match a path and everything below it.
func subtreePatterns(pattern string) []string {
	return []string{pattern, pattern + "/**"}
}

// daggerPatternMeta are the characters Dagger's include and exclude patterns
// give a meaning; a backslash makes each literal.
var daggerPatternMeta = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)

// literalPattern is a pattern that matches exactly one repository path, however
// the path is spelled. Declared paths are already literal; the paths git
// reports as ignored can be named anything.
func literalPattern(path string) string {
	escaped := daggerPatternMeta.Replace(filepath.ToSlash(path))
	if strings.HasPrefix(escaped, "!") {
		escaped = `\` + escaped
	}
	return escaped
}

// importExcludes walks the file set a Dagger import carries and returns the
// exclude patterns that leave out what the set omits, the ignored paths no
// executor reads, so the container holds exactly what the key hashed. It
// also rejects aliases before Dagger imports anything: a symlink in declared
// content could expose another part of the checkout. A symlink in ignored
// content the Go toolchain loads is imported as a link, as the key records it.
// Private files are left to daggerExcludes.
func importExcludes(ctx context.Context, set fileSet) ([]string, error) {
	dir, err := os.OpenRoot(set.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()

	var out []string
	err = set.walk(ctx, dir, func(rel string, info fs.FileInfo, scope pathScope) error {
		switch {
		case info == nil:
		case privateSourcePath(rel):
			if info.IsDir() {
				return fs.SkipDir
			}
		case scope == scopeOmitted:
			out = append(out, subtreePatterns(literalPattern(rel))...)
		case scope == scopeDeclared && info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("Dagger source contains symlink %q; declare the real input path or exclude the link", rel)
		}
		return nil
	})
	return out, err
}

// daggerSource imports the check's file set: its declared inputs, less its
// excludes, the private files, and the ignored paths the set omits.
func daggerSource(ctx context.Context, client *dagger.Client, req Request) (*dagger.Directory, error) {
	includes, err := daggerIncludes(req.Target.Inputs)
	if err != nil {
		return nil, err
	}
	set := targetFiles(req)
	omitted, err := importExcludes(ctx, set)
	if err != nil {
		return nil, err
	}
	return client.Host().Directory(set.Root, dagger.HostDirectoryOpts{
		Include: includes,
		Exclude: append(daggerExcludes(set.Excludes), omitted...),
	}), nil
}
