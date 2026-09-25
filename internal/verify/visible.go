package verify

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// visibleFiles lists the regular files a check over the whole source sees: the
// files under the target's declared inputs, less its excludes and the private
// paths (.git, .env files) the Dagger path never imports. That is exactly what
// the Dagger path's source holds, so both executors read the same files. A
// symlink is never followed; the Dagger path refuses one in a declared input.
// skip, when set, drops every file below a directory it names, at any depth
// from the repository root, including above a declared input. Paths are
// repository-relative with forward slashes, sorted.
func visibleFiles(source string, inputs, excludes []string, skip func(name string) bool) ([]string, error) {
	seen := map[string]bool{}
	for _, input := range inputs {
		err := filepath.WalkDir(filepath.Join(source, input), func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			if rel != "." && (privateSourcePath(rel) || excluded(rel, excludes) || (entry.IsDir() && skip != nil && skip(entry.Name()))) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() && !skippedDir(filepath.Dir(rel), skip) {
				seen[filepath.ToSlash(rel)] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	slices.Sort(files)
	return files, nil
}

// skippedDir reports whether skip names any component of a repository-relative
// directory.
func skippedDir(dir string, skip func(name string) bool) bool {
	if skip == nil || dir == "." {
		return false
	}
	return slices.ContainsFunc(strings.Split(filepath.ToSlash(dir), "/"), skip)
}

// stageFiles copies files, repository-relative, from source into a new
// temporary directory and returns it with a function that removes it. A tool
// that scans a directory rather than a list of files runs there natively, so it
// can neither read an undeclared or excluded file nor discover a
// configuration the Dagger path would not have.
func stageFiles(source string, files []string) (string, func(), error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return "", func() {}, err
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup.

	dir, err := os.MkdirTemp("", "levenshtein-stage-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	for _, file := range files {
		rel := filepath.FromSlash(file)
		info, err := root.Stat(rel)
		if err == nil {
			err = copyFile(root, rel, filepath.Join(dir, rel), info.Mode().Perm())
		}
		if err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("staging %s: %w", file, err)
		}
	}
	return dir, cleanup, nil
}
