package verify

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileSet is the one answer to which files a check consumes: the paths under
// a root's declared inputs, less its excludes, enumerated the way its
// discovery says. The cache fingerprint hashes exactly this set, the Dagger
// import is built from it (daggerSource), and the native scanners read it
// (visibleFiles). Where they deliberately differ, such as leaving private
// files out of what a scanner or a container sees, that is a named filter
// over this one enumeration, never a second walk.
type fileSet struct {
	Root      string
	Inputs    []string
	Excludes  []string
	Discovery DiscoveryKind
}

// goToolchainKinds run the Go toolchain over the target, natively or in the
// container. It reads every file in a package directory, and whatever a test or
// a //go:embed pattern opens below it, including generated code the work tree
// ignores (a gitignored *.pb.go, a vendor/ tree, a generated SDK), so no
// listing that leaves ignored files out can describe what these kinds consume.
var goToolchainKinds = map[CheckKind]bool{
	CheckGoLint:     true,
	CheckGoVet:      true,
	CheckGoMod:      true,
	CheckGoTest:     true,
	CheckGoHTTP:     true,
	CheckGoSQL:      true,
	CheckGoVuln:     true,
	CheckGoImports:  true,
	CheckGoGenerate: true,
	CheckGoApidiff:  true,
	CheckGoMutation: true,
}

// enumeratesFilesystem reports whether a check of kind enumerates its inputs
// from the filesystem whatever its target's discovery says. Planning records
// the answer in the planned target, so the key, the Dagger import, and the
// report all name the enumeration that was used.
func enumeratesFilesystem(kind CheckKind) bool {
	return goToolchainKinds[kind]
}

// targetFiles is the file set a planned check's target declares.
func targetFiles(req Request) fileSet {
	return fileSet{
		Root:      req.Source,
		Inputs:    req.Target.Inputs,
		Excludes:  req.Target.Exclude,
		Discovery: req.Target.Discovery,
	}
}

// visitFunc receives one enumerated path, relative to the root with the OS
// separator. info is nil for a declared input that does not exist. Returning
// fs.SkipDir for a directory leaves its contents out.
type visitFunc func(rel string, info fs.FileInfo) error

// under reports whether path is input or lies below it. Every "inputs minus
// excludes" decision goes through it, so a declared path means the same thing
// to the key, the Dagger import, and the native readers.
func under(path, input string) bool {
	return input == "." || path == input || strings.HasPrefix(path, input+string(filepath.Separator))
}

// walk enumerates the set, calling visit for each path. Under git discovery the
// paths are the work tree's own listing: directories have no entries of their
// own, and a listed file absent from disk is skipped rather than reported
// missing, so a work tree enumerates like a fresh clone. A listed directory (a
// submodule's gitlink or an untracked nested repository) is walked in full,
// because git lists nothing inside it. Outside a work tree, or under
// filesystem discovery, every path under each input is walked.
//
// A symlink is reported, never followed, including one above a declared input:
// an input reached through a link is an alias, which the source symlink
// policy refuses to fingerprint.
func (s fileSet) walk(ctx context.Context, dir *os.Root, visit visitFunc) error {
	var listed []string
	tracked := false
	if s.Discovery == DiscoveryGit {
		listed, tracked = gitFiles(ctx, s.Root)
	}

	for _, input := range s.Inputs {
		if !relative(input) {
			return fmt.Errorf("invalid input %q", input)
		}
		if excluded(input, s.Excludes) {
			continue
		}

		reached, nested, err := reach(dir, listed, input, visit)
		if err != nil {
			return err
		}
		switch {
		case !reached:
		case tracked && !nested:
			err = s.walkListing(dir, listed, input, visit)
		default:
			err = s.walkTree(dir, input, visit)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// reach inspects each directory above a declared input. A symlink there is
// visited itself and the input is not enumerated through it; a parent that is
// not a directory makes the input missing. nested reports a parent the listing
// names as a whole, whose contents only the filesystem can enumerate.
func reach(dir *os.Root, listed []string, input string, visit visitFunc) (reached, nested bool, err error) {
	parts := strings.Split(input, string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		parent := filepath.Join(parts[:i]...)
		info, err := dir.Lstat(parent)
		if errors.Is(err, fs.ErrNotExist) {
			return true, nested, nil
		}
		if err != nil {
			return false, false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, false, visit(parent, info)
		}
		if !info.IsDir() {
			return false, false, visit(input, nil)
		}
		if listedAt(listed, parent) {
			nested = true
		}
	}
	return true, nested, nil
}

// listedAt reports whether the sorted listing names path itself.
func listedAt(listed []string, path string) bool {
	i := sort.SearchStrings(listed, path)
	return i < len(listed) && listed[i] == path
}

// listedUnder returns the listed paths an input covers. The listing is sorted,
// so everything below a directory is one contiguous run, found by binary
// search rather than a scan of the whole listing per input.
func listedUnder(listed []string, input string) []string {
	if input == "." {
		return listed
	}

	var covered []string
	if listedAt(listed, input) {
		covered = append(covered, input)
	}
	prefix := input + string(filepath.Separator)
	lo := sort.SearchStrings(listed, prefix)
	hi := lo + sort.Search(len(listed)-lo, func(i int) bool { return !strings.HasPrefix(listed[lo+i], prefix) })
	return append(covered, listed[lo:hi]...)
}

func (s fileSet) walkListing(dir *os.Root, listed []string, input string, visit visitFunc) error {
	covered := listedUnder(listed, input)
	for _, rel := range covered {
		if excluded(rel, s.Excludes) {
			continue
		}

		info, err := dir.Lstat(rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}

		if info.IsDir() {
			err = s.walkTree(dir, rel, visit)
		} else {
			err = visit(rel, info)
		}
		if err != nil {
			return err
		}
	}
	if len(covered) > 0 {
		return nil
	}

	// A declared path git knows nothing about is missing only when it is also
	// absent from disk; an ignored one contributes nothing. One that resolves
	// only because the filesystem ignores case is neither: git spells it
	// differently, so it must be declared as git spells it.
	_, err := dir.Lstat(input)
	if errors.Is(err, fs.ErrNotExist) {
		return visit(input, nil)
	}
	if err != nil {
		return err
	}
	return spelledExactly(dir, input)
}

func (s fileSet) walkTree(dir *os.Root, path string, visit visitFunc) error {
	return fs.WalkDir(snapshotFS{FS: dir.FS(), root: dir}, filepath.ToSlash(path), func(name string, entry fs.DirEntry, err error) error {
		rel := filepath.FromSlash(name)
		if excluded(rel, s.Excludes) {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return visit(rel, nil)
		}
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		err = visit(rel, info)
		// SkipDir from a file would skip its remaining siblings.
		if errors.Is(err, fs.SkipDir) && !info.IsDir() {
			return nil
		}
		return err
	})
}

// spelling returns path as the filesystem spells the part of it that exists.
// On a case-insensitive filesystem a declared "Src" opens "src", but git, the
// Dagger import's patterns, and the exclude prefixes all compare exact bytes.
// A part that does not exist, or a directory that cannot be read, keeps the
// declared spelling from there on; enumeration reports real errors itself.
func spelling(dir *os.Root, path string) string {
	parts := strings.Split(path, string(filepath.Separator))
	for i, part := range parts {
		entries, err := fs.ReadDir(dir.FS(), filepath.ToSlash(filepath.Join(append([]string{"."}, parts[:i]...)...)))
		if err != nil {
			break
		}

		found := ""
		for _, entry := range entries {
			if entry.Name() == part {
				found = part
				break
			}
			if found == "" && strings.EqualFold(entry.Name(), part) {
				found = entry.Name()
			}
		}
		if found == "" {
			break
		}
		parts[i] = found
	}
	return filepath.Join(parts...)
}

// spelledAsOnDisk rejects, at plan time, a declared path that resolves in source
// only under another spelling.
func spelledAsOnDisk(source string, paths []string) error {
	dir, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }() // Read-only directory handle.

	for _, path := range paths {
		if err := spelledExactly(dir, path); err != nil {
			return err
		}
	}
	return nil
}

func resolves(dir *os.Root, path string) bool {
	_, err := dir.Lstat(path)
	return err == nil
}

// spelledExactly rejects a path that resolves only under another spelling. A
// path that does not resolve at all is merely missing, and enumeration reports
// one that cannot be inspected.
func spelledExactly(dir *os.Root, path string) error {
	if !resolves(dir, path) {
		return nil
	}
	if disk := spelling(dir, path); disk != path {
		return fmt.Errorf("path %q is spelled %q on disk; declare it exactly as the repository spells it", path, disk)
	}
	return nil
}
