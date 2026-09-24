package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// goGenerate runs go generate ./... in a scratch copy of the target's inputs
// and reports every file it would add, change, or delete, with the diff. The
// working tree is never written: generators run in the copy, which is removed
// afterwards.
func (n *Native) goGenerate(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	if _, err := os.Stat(filepath.Join(work.Dir, "go.mod")); err != nil {
		return nil, toolRun{}, fmt.Errorf("module %q needs a readable go.mod: %w", req.Target.Dir, err)
	}
	binary, err := build(ctx, req, work, helperGocheck)
	if err != nil {
		return nil, toolRun{}, err
	}

	scratch, err := os.MkdirTemp("", "levenshtein-generate-")
	if err != nil {
		return nil, toolRun{}, err
	}
	defer func() { _ = os.RemoveAll(scratch) }() // Scratch copy cleanup.
	if err := copyInputs(ctx, req, scratch); err != nil {
		return nil, toolRun{}, fmt.Errorf("copying the target's inputs: %w", err)
	}

	// Generators see the copy wherever they look: the workspace the container
	// would see, and the source and workspace a native command is told about.
	env := goEnv(work.Env, []string{
		"GOWORK=" + scratchWorkspace(req, work.Dir, scratch),
		"LEVENSHTEIN_SOURCE=" + scratch,
		"LEVENSHTEIN_WORKSPACE=" + filepath.Join(scratch, req.Target.Workspace),
	})
	args := []string{binary, "generate", "-root=" + scratch, "-module=" + filepath.ToSlash(req.Target.Dir)}
	return gocheckRun(ctx, CheckGoGenerate, scratch, args, env)
}

// scratchWorkspace is the copy's counterpart of the go.work the target is
// analyzed with, or "off".
func scratchWorkspace(req Request, dir, scratch string) string {
	gowork := workspace(req, dir)
	if gowork == "off" {
		return gowork
	}
	rel, err := filepath.Rel(req.Source, gowork)
	if err != nil || !filepath.IsLocal(rel) {
		return "off"
	}
	return filepath.Join(scratch, rel)
}

// copyInputs copies the target's declared inputs into dest, laid out as they
// are in the source, so a check that has to change files can work on the copy.
// Like the Dagger import, it leaves out the target's excludes and private
// files such as .git and .env. With git discovery it copies only the files the
// fingerprint covers, which leaves out the build output and dependency
// directories the repository ignores; with filesystem discovery it copies
// everything under each input. A relative symlink is copied as a link; one
// that leads outside the copy is an error.
func copyInputs(ctx context.Context, req Request, dest string) error {
	root, err := os.OpenRoot(req.Source)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup.

	copied := map[string]bool{}
	var listed []string
	tracked := false
	if req.Target.Discovery == DiscoveryGit {
		listed, tracked = gitFiles(ctx, req.Source)
	}
	for _, input := range req.Target.Inputs {
		if !tracked {
			if err := copyTree(root, input, dest, req.Target.Exclude, copied); err != nil {
				return err
			}
			continue
		}
		for _, rel := range listed {
			if input == "." || rel == input || strings.HasPrefix(rel, input+string(filepath.Separator)) {
				if err := copyTree(root, rel, dest, req.Target.Exclude, copied); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// copyTree copies one source-relative path into dest, walking it when it is a
// directory. A declared input that does not exist is skipped, as fingerprinting
// records it as missing rather than failing.
func copyTree(root *os.Root, path, dest string, excludes []string, copied map[string]bool) error {
	return fs.WalkDir(snapshotFS{FS: root.FS(), root: root}, filepath.ToSlash(path), func(name string, entry fs.DirEntry, err error) error {
		rel := filepath.FromSlash(name)
		if privateSourcePath(rel) || excluded(rel, excludes) {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if copied[rel] {
			return nil
		}
		copied[rel] = true

		info, err := entry.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o700|info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := root.Readlink(rel)
			if err != nil {
				return err
			}
			// A generator writes through links in the copy, so one that leads
			// out of it could change the working tree the check promises to
			// leave alone.
			if filepath.IsAbs(link) || !filepath.IsLocal(filepath.Join(filepath.Dir(rel), link)) {
				return fmt.Errorf("%s is a symlink to %s, outside the target's inputs; declare the real path", rel, link)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(root, rel, target, info.Mode().Perm())
		}
		return nil // Sockets, devices and pipes are not source.
	})
}

func copyFile(root *os.Root, rel, target string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	source, err := root.Open(rel)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }() // Read-only file cleanup.

	copied, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(copied, source); err != nil {
		_ = copied.Close()
		return err
	}
	return copied.Close()
}
