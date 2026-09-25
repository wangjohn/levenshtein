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
// import is built from it (daggerSource), and the native readers take it
// (visibleFiles, copyInputs). Where they deliberately differ, such as leaving
// private files out of what a scanner or a container sees, that is a named
// filter over this one enumeration, never a second walk.
type fileSet struct {
	Root      string
	Inputs    []string
	Excludes  []string
	Discovery DiscoveryKind
	// GoToolchain widens git discovery with the ignored paths the Go toolchain
	// can load (see goLoader), because it reads them whatever git lists.
	GoToolchain bool
}

// pathScope says why an enumerated path is in a file set.
type pathScope string

const (
	// scopeDeclared is the target's own content: listed by git, or walked from
	// disk. A symlink here is refused by the key and by the Dagger import.
	scopeDeclared pathScope = "declared"
	// scopeIgnored is a path the work tree ignores that the Go toolchain can
	// load. A symlink here is hashed by its link text and never followed.
	scopeIgnored pathScope = "ignored"
	// scopeOmitted is an ignored path the set leaves out. It is reported once,
	// never descended, so an import can exclude it.
	scopeOmitted pathScope = "omitted"
)

// targetFiles is the file set a planned check's target declares.
func targetFiles(req Request) fileSet {
	return fileSet{
		Root:        req.Source,
		Inputs:      req.Target.Inputs,
		Excludes:    req.Target.Exclude,
		Discovery:   req.Target.Discovery,
		GoToolchain: readsGoToolchain(req.Check.Kind),
	}
}

// visitFunc receives one enumerated path, relative to the root with the OS
// separator, and why it is in the set. info is nil for a declared input that
// does not exist. Returning fs.SkipDir for a directory leaves its contents out.
type visitFunc func(rel string, info fs.FileInfo, scope pathScope) error

// under reports whether path is input or lies below it. Every "inputs minus
// excludes" decision goes through it, so a declared path means the same thing
// to the key, the Dagger import, and the native readers.
func under(path, input string) bool {
	return input == "." || path == input || strings.HasPrefix(path, input+string(filepath.Separator))
}

// walk enumerates the set, calling visit for each path. Under git discovery the
// paths are the work tree's own listing, as session remembers it: directories
// have no entries of their own, and a listed file absent from disk is skipped
// rather than reported missing, so a work tree enumerates like a fresh clone.
// A listed directory (a submodule's gitlink or an untracked nested repository)
// is walked in full, because git lists nothing inside it. Each ignored path is
// then reported once, omitted, or for a Go toolchain set walked as far as the
// toolchain can load it. Outside a work tree, or under filesystem discovery,
// every path under each input is walked.
//
// A symlink is reported, never followed, including one above a declared input:
// an input reached through a link is an alias, which the source symlink
// policy refuses to fingerprint.
func (s fileSet) walk(ctx context.Context, session *Session, dir *os.Root, visit visitFunc) error {
	// One listing answers both what git lists and what it ignores, so the two
	// halves of a walk cannot come from different runs of git.
	found := &gitListing{}
	if s.Discovery == DiscoveryGit {
		found = session.listing(ctx, s.Root)
	}
	listed, ignored, tracked := found.files, found.ignored, found.files != nil
	loader := newGoLoader(dir, &found.goDirectories)

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
			if err == nil {
				err = s.walkIgnored(dir, ignored, input, loader, visit)
			}
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
			return false, false, visit(parent, info, scopeDeclared)
		}
		if !info.IsDir() {
			return false, false, visit(input, nil, scopeDeclared)
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
			err = visit(rel, info, scopeDeclared)
		}
		if err != nil {
			return err
		}
	}
	if len(covered) > 0 {
		return nil
	}

	// A declared path git knows nothing about is missing only when it is also
	// absent from disk; an ignored one is walkIgnored's. One that resolves
	// only because the filesystem ignores case is neither: git spells it
	// differently, so it must be declared as git spells it.
	_, err := dir.Lstat(input)
	if errors.Is(err, fs.ErrNotExist) {
		return visit(input, nil, scopeDeclared)
	}
	if err != nil {
		return err
	}
	return spelledExactly(dir, input)
}

// walkIgnored reports the ignored paths an input covers: those below it, or
// the input itself when an ignored directory holds it.
func (s fileSet) walkIgnored(dir *os.Root, ignored []string, input string, loader *goLoader, visit visitFunc) error {
	paths := listedUnder(ignored, input)
	parts := strings.Split(input, string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		if listedAt(ignored, filepath.Join(parts[:i]...)) {
			paths = []string{input}
			break
		}
	}

	held := ""
	for _, rel := range paths {
		// git can name an ignored directory and an ignored file inside it.
		if held != "" && under(rel, held) {
			continue
		}
		held = rel
		if excluded(rel, s.Excludes) {
			continue
		}

		info, err := dir.Lstat(rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		switch {
		case err != nil:
		case !s.GoToolchain:
			err = visit(rel, info, scopeOmitted)
		case info.IsDir():
			err = loader.walk(rel, s.Excludes, visit)
		default:
			err = visit(rel, info, loader.fileScope(rel))
		}
		if err != nil {
			return err
		}
	}
	return nil
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
			return visit(rel, nil, scopeDeclared)
		}
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		return skipOnlyDirs(visit(rel, info, scopeDeclared), info)
	})
}

// skipOnlyDirs keeps fs.SkipDir from a file, which would skip its remaining
// siblings, from reaching fs.WalkDir.
func skipOnlyDirs(err error, info fs.FileInfo) error {
	if errors.Is(err, fs.SkipDir) && !info.IsDir() {
		return nil
	}
	return err
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
