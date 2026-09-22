package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func excluded(path string, excludes []string) bool {
	if filepath.Base(path) == ".git" {
		return true
	}
	for _, p := range excludes {
		if path == p || strings.HasPrefix(path, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// WalkDir must inspect a declared root symlink itself, just as it does child entries.
type snapshotFS struct {
	fs.FS
	root *os.Root
}

func (s snapshotFS) Stat(name string) (fs.FileInfo, error) {
	return s.root.Lstat(filepath.FromSlash(name))
}

// snapshotRequest describes one content hash over a root: which declared paths
// it covers, which paths it must skip, whether it records mutable outputs, and
// how the files under those paths are enumerated.
type snapshotRequest struct {
	Root      string
	Paths     []string
	Excludes  []string
	Outputs   bool
	Discovery DiscoveryKind
}

// hashTarget is a regular file whose content still has to be read. Info is the
// stat the enumeration already took, so hashing needs no second Lstat.
type hashTarget struct {
	Path string
	Info fs.FileInfo
}

// Snapshot hashes content, names and modes, including untracked files and missing
// paths. Source symlinks disable result reuse; output snapshots retain link text.
func snapshot(req snapshotRequest) (string, error) {
	dir, err := os.OpenRoot(req.Root)
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }() // Directory handle cleanup; writes are closed separately.

	// Mutable outputs are generated files the work tree usually ignores, so they
	// are always enumerated from the filesystem.
	var listed []string
	tracked := false
	if req.Discovery == DiscoveryGit && !req.Outputs {
		listed, tracked = gitFiles(req.Root)
	}

	entries := map[string]string{}
	var files []hashTarget
	for _, path := range req.Paths {
		if !relative(path) {
			return "", fmt.Errorf("invalid input %q", path)
		}

		var err error
		if tracked {
			err = walkListing(dir, listed, path, req, entries, &files)
		} else {
			err = walkTree(dir, path, req, entries, &files)
		}
		if err != nil {
			return "", err
		}
	}
	if err := hashFiles(req.Root, dir, files, entries); err != nil {
		return "", err
	}
	return digest(entries), nil
}

// record classifies one enumerated path. Regular files are queued rather than
// read, so every content hash in one snapshot can run at once.
func record(dir *os.Root, req snapshotRequest, rel string, info fs.FileInfo, entries map[string]string, files *[]hashTarget) error {
	if info.Mode()&os.ModeSymlink != 0 {
		if !req.Outputs {
			return fmt.Errorf("source symlink %q requires fresh execution", rel)
		}
		link, err := dir.Readlink(rel)
		if err != nil {
			return err
		}
		entries[rel] = digest([]string{"symlink", link})
		return nil
	}
	if info.IsDir() {
		entries[rel] = "directory"
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported input file %q", rel)
	}

	*files = append(*files, hashTarget{Path: rel, Info: info})
	return nil
}

func walkTree(dir *os.Root, path string, req snapshotRequest, entries map[string]string, files *[]hashTarget) error {
	return fs.WalkDir(snapshotFS{FS: dir.FS(), root: dir}, filepath.ToSlash(path), func(name string, entry fs.DirEntry, err error) error {
		rel := filepath.FromSlash(name)
		if excluded(rel, req.Excludes) {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if os.IsNotExist(err) {
			entries[rel] = "missing"
			return nil
		}
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		return record(dir, req, rel, info, entries, files)
	})
}

// walkListing covers the declared input with the work tree's own file list.
// Directories produce no entries of their own in this mode, and a tracked file
// that is not on disk is skipped rather than recorded as missing, so a tree
// matches a fresh clone of the same content.
func walkListing(dir *os.Root, listed []string, path string, req snapshotRequest, entries map[string]string, files *[]hashTarget) error {
	covered := false
	for _, rel := range listed {
		if path != "." && rel != path && !strings.HasPrefix(rel, path+string(filepath.Separator)) {
			continue
		}
		covered = true
		if excluded(rel, req.Excludes) {
			continue
		}

		info, err := dir.Lstat(rel)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}

		// The listing names files, so a directory here is a submodule's gitlink or
		// an untracked nested repository, whose contents git does not list. Walk
		// it, or edits inside it would never change the fingerprint.
		if info.IsDir() {
			err = walkTree(dir, rel, req, entries, files)
		} else {
			err = record(dir, req, rel, info, entries, files)
		}
		if err != nil {
			return err
		}
	}
	if covered {
		return nil
	}

	// A declared path git knows nothing about is still missing only when it is
	// also absent from disk; an ignored directory contributes no entries.
	_, err := dir.Lstat(path)
	if os.IsNotExist(err) {
		entries[path] = "missing"
		return nil
	}
	return err
}

// hashFiles reads every queued file, bounded by the available parallelism. The
// resulting entries map does not depend on completion order.
func hashFiles(root string, dir *os.Root, files []hashTarget, entries map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	stats.prepare(root)
	values := make([]string, len(files))
	var group errgroup.Group
	group.SetLimit(runtime.GOMAXPROCS(0))
	for i, file := range files {
		group.Go(func() error {
			value, err := fileEntry(root, dir, file)
			values[i] = value
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}

	for i, file := range files {
		entries[file.Path] = values[i]
	}
	return nil
}

func fileEntry(root string, dir *os.Root, file hashTarget) (string, error) {
	key := statKey{Root: root, Path: file.Path}
	current := statOf(file.Info)
	if value, ok := stats.lookup(key, current); ok {
		return value, nil
	}

	// The read starts now. A write in the same timestamp tick as this moment is
	// what the persisted cache's racy window guards against.
	hashedAt := time.Now()
	f, err := dir.Open(file.Path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, readErr := io.Copy(h, f)
	closeErr := f.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}

	value := fmt.Sprintf("%o:%x", file.Info.Mode().Perm(), h.Sum(nil))
	stats.store(key, current, value, hashedAt)
	return value, nil
}

func outputPaths(req Request) []string {
	out := append([]string{}, req.Check.artifacts()...)
	for _, stage := range req.stages() {
		out = append(out, stage.definition.Outputs...)
	}
	return out
}

// implementationKey separates memoized snapshots per shared checkout so
// independent roots, including each test's own temporary directory, never share
// an entry. The executor kind and whether the check runs the shared checkout's
// own tools select which paths the snapshot covers.
type implementationKey struct {
	Shared   string
	Executor ExecutorKind
	Runner   bool
}

// implementations memoizes the shared checkout's snapshot for the life of the
// process. A run pins one Levenshtein checkout, and that checkout does not
// change while the run executes, but fingerprint is called up to four times per
// check and every preparation stage hashes it again; walking go.mod, go.sum,
// cmd, internal and the Dagger runtime each time dominated cache lookups.
//
// The trade-off this records: editing the shared checkout while a run is in
// flight does not change its fingerprints. Start a new run after changing the
// pinned checkout.
var implementations sync.Map

func implementation(req Request) (string, error) {
	// A shared Go check runs the shared checkout's linter and house rules
	// (runner/lint), reads its rule list (runner/toolchain.json) and builds its
	// pinned tools (runner/tools) on either executor, so all of runner/ decides
	// its verdict. Without it, bumping the pinned checkout to a revision that
	// adds rules would reuse results the new rules never saw.
	runner := req.Environment.Executor == ExecutorDagger || sharedGoChecks[req.Check.Kind]
	key := implementationKey{Shared: req.Shared, Executor: req.Environment.Executor, Runner: runner}
	if memoized, ok := implementations.Load(key); ok {
		return memoized.(string), nil
	}

	paths := []string{"go.mod", "go.sum", "cmd", "internal"}
	switch {
	case req.Environment.Executor == ExecutorDagger:
		paths = append(paths, ".dagger-version", "dagger.json", "runner", "sdk")
	case runner:
		paths = append(paths, "runner")
	}
	// The shared checkout is also shipped as a release archive with no work
	// tree, so it is always enumerated the same way wherever it came from.
	impl, err := snapshot(snapshotRequest{Root: req.Shared, Paths: paths, Discovery: DiscoveryFilesystem})
	if err != nil {
		return "", err
	}

	implementations.Store(key, impl)
	return impl, nil
}

func fingerprint(req Request) (string, error) {
	paths := append([]string{}, req.Target.Inputs...)
	for _, stage := range req.stages() {
		paths = append(paths, stage.definition.Inputs...)
	}
	sort.Strings(paths)
	source, err := snapshot(snapshotRequest{
		Root:      req.Source,
		Paths:     paths,
		Excludes:  append(outputPaths(req), req.Target.Exclude...),
		Discovery: req.Target.Discovery,
	})
	if err != nil {
		return "", err
	}
	impl, err := implementation(req)
	if err != nil {
		return "", err
	}

	req.RerunChecks = false
	var env []string
	toolchain := ""
	if req.Environment.Executor == ExecutorNative {
		env = nativeEnv(req, req.Check.env())
		toolchain, err = hostToolchain(req, env)
		if err != nil {
			return "", err
		}
	}
	return digest(struct {
		Check          PlannedCheck
		Source         string
		Implementation string
		OS             string
		Arch           string
		Env            []string
		Toolchain      string `json:",omitempty"`
	}{req.PlannedCheck, source, impl, runtime.GOOS, runtime.GOARCH, env, toolchain}), nil
}

// hostToolchain identifies the Go a native shared check will actually use. The
// Dagger path pins its toolchain through the image digest in the implementation
// snapshot; the native path has nothing equivalent, so the host's own version,
// OS and architecture join the key. Other native kinds contribute nothing, so
// their existing cache entries keep their identity.
func hostToolchain(req Request, env []string) (string, error) {
	if !sharedGoChecks[req.Check.Kind] {
		return "", nil
	}

	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return "", err
	}
	identity, err := toolchainIdentity(context.Background(), dir, env)
	if err != nil {
		return "", err
	}
	return digest(identity), nil
}
