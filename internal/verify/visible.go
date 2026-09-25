package verify

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// visibleFiles lists the regular files a check over the whole source sees: the
// target's file set (see fileSet), which is what its key hashed, less the
// private paths (.git, .env files) the Dagger path never imports. That is
// exactly what the Dagger path's source holds, so both executors read the same
// files: under git discovery neither reads a file the work tree ignores. A
// symlink is never followed; the key refuses to hash one and the Dagger path
// refuses to import one. skip, when set, drops every file below a directory it
// names, at any depth from the repository root, including above a declared
// input. Paths are repository-relative with forward slashes, sorted.
func visibleFiles(ctx context.Context, req Request, skip func(name string) bool) ([]string, error) {
	dir, err := os.OpenRoot(req.Source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }() // Read-only directory handle.

	seen := map[string]bool{}
	err = targetFiles(req).walk(ctx, dir, func(rel string, info fs.FileInfo) error {
		switch {
		case info == nil:
		case privateSourcePath(rel) || (info.IsDir() && skip != nil && skip(info.Name())):
			if info.IsDir() {
				return fs.SkipDir
			}
		case info.Mode().IsRegular() && !skippedDir(filepath.Dir(rel), skip):
			seen[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
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
