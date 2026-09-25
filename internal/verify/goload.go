package verify

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// goSourceExtensions are the file extensions go/build hands to the compiler,
// cgo, the assembler, or the linker, and so reads from a package directory.
var goSourceExtensions = []string{".go", ".c", ".cc", ".cxx", ".cpp", ".m", ".h", ".hh", ".hpp", ".hxx", ".f", ".F", ".for", ".f90", ".s", ".S", ".sx", ".swig", ".swigcxx", ".syso"}

// goModuleFiles are read wherever they sit: a nested module, a workspace.
var goModuleFiles = []string{"go.mod", "go.sum", "go.work", "go.work.sum"}

// goLoader decides which paths the work tree ignores the Go toolchain can
// still load, so a Go toolchain file set hashes them and leaves out the rest.
//
// cmd/go gives no ignored directory a pass by name: package patterns such as
// ./... skip directories named testdata or beginning with "." or "_", nested
// modules, and go.mod ignore directives, but an explicit import path still
// loads a package from any of them, and a //go:embed file name can reach into
// "." and "_" directories below its package. What is provable is by content:
//
//   - a directory whose subtree holds no .go file holds no package, so nothing
//     in it is compiled or embedded by a package of its own;
//   - a file can be embedded only by a package in its directory or above, so a
//     path with no //go:embed directive in any .go file of those directories is
//     out of every embed's reach;
//   - testdata is what tests read by convention, so it is always loadable.
//
// Everything else ignored is left out. The residual risk is a test that opens
// an ignored file outside testdata with a path of its own making, or cgo
// including an ignored header from another directory, which no enumeration
// short of running the check can see.
type goLoader struct {
	dir *os.Root
	// scans memoizes goDirectories per ignored directory, for the life of the
	// git listing the walk enumerates.
	scans *sync.Map
	// embeds memoizes, per directory, whether a .go file in it embeds.
	embeds map[string]bool
}

func newGoLoader(dir *os.Root, scans *sync.Map) *goLoader {
	return &goLoader{dir: dir, scans: scans, embeds: map[string]bool{}}
}

// fileScope classifies an ignored file.
func (l *goLoader) fileScope(rel string) pathScope {
	name := filepath.Base(rel)
	if slices.Contains(goSourceExtensions, filepath.Ext(name)) || slices.Contains(goModuleFiles, name) || inTestdata(rel) || l.embedded(filepath.Dir(rel)) {
		return scopeIgnored
	}
	return scopeOmitted
}

// walk reports what an ignored directory holds that the toolchain can load. A
// subdirectory it cannot load anything from is reported once, omitted, and not
// descended; kept directories have no entries of their own, as under git
// discovery.
func (l *goLoader) walk(root string, excludes []string, visit visitFunc) error {
	bearing, err := l.goDirectories(root)
	if err != nil {
		return err
	}

	return fs.WalkDir(snapshotFS{FS: l.dir.FS(), root: l.dir}, filepath.ToSlash(root), func(name string, entry fs.DirEntry, err error) error {
		rel := filepath.FromSlash(name)
		if excluded(rel, excludes) {
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

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return skipOnlyDirs(visit(rel, info, l.fileScope(rel)), info)
		}
		if bearing[rel] || inTestdata(rel) || l.embedded(rel) {
			return nil
		}
		if err := visit(rel, info, scopeOmitted); err != nil {
			return err
		}
		return filepath.SkipDir
	})
}

// goDirectories names every directory under root, root included, whose
// subtree holds a .go file. It reads directory entries only: no file is opened
// or statted, and no symlink is followed.
func (l *goLoader) goDirectories(root string) (map[string]bool, error) {
	if memoized, ok := l.scans.Load(root); ok {
		return memoized.(map[string]bool), nil
	}

	bearing := map[string]bool{}
	err := fs.WalkDir(l.dir.FS(), filepath.ToSlash(root), func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(name) != ".go" {
			return nil
		}
		for dir := filepath.Dir(filepath.FromSlash(name)); under(dir, root) && !bearing[dir]; dir = filepath.Dir(dir) {
			bearing[dir] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	l.scans.Store(root, bearing)
	return bearing, nil
}

// embedded reports whether dir, or a directory above it, holds a .go file with
// a //go:embed directive, which could name a file in dir.
func (l *goLoader) embedded(dir string) bool {
	for {
		if l.embedsIn(dir) {
			return true
		}
		if dir == "." {
			return false
		}
		dir = filepath.Dir(dir)
	}
}

func (l *goLoader) embedsIn(dir string) bool {
	if found, ok := l.embeds[dir]; ok {
		return found
	}

	found := false
	entries, _ := fs.ReadDir(l.dir.FS(), filepath.ToSlash(dir)) // An unreadable directory holds nothing to embed with.
	for _, entry := range entries {
		if found || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		source, err := fs.ReadFile(l.dir.FS(), filepath.ToSlash(filepath.Join(dir, entry.Name())))
		found = err == nil && bytes.Contains(source, []byte("//go:embed"))
	}
	l.embeds[dir] = found
	return found
}

// inTestdata reports whether a path lies in a testdata directory.
func inTestdata(rel string) bool {
	return slices.Contains(strings.Split(rel, string(filepath.Separator)), "testdata")
}
