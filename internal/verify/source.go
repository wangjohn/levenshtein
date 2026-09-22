package verify

import (
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
			path := filepath.ToSlash(input)
			includes = append(includes, path, path+"/**")
		}
	}
	return includes, nil
}

func privateSourcePath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
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
		slash := filepath.ToSlash(path)
		out = append(out, slash, slash+"/**")
	}
	return out
}

// Reject aliases before asking Dagger to import files. A path inside an allowed
// directory must not expose another part of the checkout through a symlink.
func validateDaggerSource(source string, inputs, excludes []string) error {
	dir, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	for _, input := range inputs {
		if privateSourcePath(input) || excluded(input, excludes) {
			continue
		}
		prefix := ""
		for _, part := range strings.Split(input, string(filepath.Separator)) {
			prefix = filepath.Join(prefix, part)
			info, err := dir.Lstat(prefix)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("Dagger input %q contains symlink %q; declare the real input path", input, prefix)
			}
		}

		err := fs.WalkDir(snapshotFS{FS: dir.FS(), root: dir}, filepath.ToSlash(input), func(path string, entry fs.DirEntry, err error) error {
			if privateSourcePath(path) || excluded(filepath.FromSlash(path), excludes) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("Dagger input %q contains symlink %q; declare the real input path", input, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func daggerSource(client *dagger.Client, source string, inputs, excludes []string) (*dagger.Directory, error) {
	includes, err := daggerIncludes(inputs)
	if err != nil {
		return nil, err
	}
	if err := validateDaggerSource(source, inputs, excludes); err != nil {
		return nil, err
	}
	return client.Host().Directory(source, dagger.HostDirectoryOpts{
		Include: includes,
		Exclude: daggerExcludes(excludes),
	}), nil
}
