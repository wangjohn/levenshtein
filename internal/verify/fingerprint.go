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
		if under(path, p) {
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
	// GoToolchain adds the ignored paths the Go toolchain can load (see
	// fileSet).
	GoToolchain bool
}

// hashTarget is a regular file whose content still has to be read. Info is the
// stat the enumeration already took, so hashing needs no second Lstat.
type hashTarget struct {
	Path string
	Info fs.FileInfo
}

// Snapshot hashes content, names and modes, including untracked files and missing
// paths. Source symlinks disable result reuse; output snapshots retain link text.
// The session supplies the git listing and the file stat memo.
func (s *Session) snapshot(ctx context.Context, req snapshotRequest) (string, error) {
	entries, err := s.snapshotEntries(ctx, req)
	if err != nil {
		return "", err
	}
	return digest(entries), nil
}

// snapshotEntries is what snapshot digests: one entry per enumerated path,
// keyed by its root-relative path.
func (s *Session) snapshotEntries(ctx context.Context, req snapshotRequest) (map[string]string, error) {
	dir, err := os.OpenRoot(req.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }() // Directory handle cleanup; writes are closed separately.

	// Mutable outputs are generated files the work tree usually ignores, so they
	// are always enumerated from the filesystem.
	discovery := req.Discovery
	if req.Outputs {
		discovery = DiscoveryFilesystem
	}
	set := fileSet{Root: req.Root, Inputs: req.Paths, Excludes: req.Excludes, Discovery: discovery, GoToolchain: req.GoToolchain}

	entries := map[string]string{}
	var files []hashTarget
	err = set.walk(ctx, s, dir, func(rel string, info fs.FileInfo, scope pathScope) error {
		return record(dir, req, rel, info, scope, entries, &files)
	})
	if err != nil {
		return nil, err
	}
	if err := hashFiles(s.stats, req.Root, dir, files, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// record classifies one enumerated path. Regular files are queued rather than
// read, so every content hash in one snapshot can run at once. An omitted path
// is not part of the key. A symlink in declared source disables result reuse;
// one in an output or in ignored content the Go toolchain can load is recorded
// by its link text and never followed.
func record(dir *os.Root, req snapshotRequest, rel string, info fs.FileInfo, scope pathScope, entries map[string]string, files *[]hashTarget) error {
	if scope == scopeOmitted {
		return nil
	}
	if info == nil {
		entries[rel] = "missing"
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !req.Outputs && scope == scopeDeclared {
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

// hashFiles reads every queued file, bounded by the available parallelism. The
// resulting entries map does not depend on completion order.
func hashFiles(stats *statStore, root string, dir *os.Root, files []hashTarget, entries map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	stats.prepare(root)
	values := make([]string, len(files))
	var group errgroup.Group
	group.SetLimit(runtime.GOMAXPROCS(0))
	for i, file := range files {
		group.Go(func() error {
			value, err := fileEntry(stats, root, dir, file)
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

func fileEntry(stats *statStore, root string, dir *os.Root, file hashTarget) (string, error) {
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

// implementation is the shared checkout's snapshot, memoized for the life of
// the session. A run pins one Levenshtein checkout, and that checkout does not
// change while the run executes, but fingerprint is called up to four times per
// check and every preparation stage hashes it again; walking go.mod, go.sum,
// cmd, internal and the Dagger runtime each time dominated cache lookups.
//
// The trade-off this records: editing the shared checkout while a run is in
// flight does not change its fingerprints. Start a new run after changing the
// pinned checkout.
func implementation(ctx context.Context, req Request) (string, error) {
	session := sessionOf(req)
	// A shared Go check runs the shared checkout's linter and house rules
	// (runner/lint), reads its rule list (runner/toolchain.json) and builds its
	// pinned tools (runner/tools) on either executor, so all of runner/ decides
	// its verdict. Without it, bumping the pinned checkout to a revision that
	// adds rules would reuse results the new rules never saw.
	runner := req.Environment.Executor == ExecutorDagger || sharedGoCheck(req.Check.Kind)
	key := implementationKey{Shared: req.Shared, Executor: req.Environment.Executor, Runner: runner}
	if memoized, ok := session.implementations.Load(key); ok {
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
	impl, err := session.snapshot(ctx, snapshotRequest{Root: req.Shared, Paths: paths, Discovery: DiscoveryFilesystem})
	if err != nil {
		return "", err
	}

	session.implementations.Store(key, impl)
	return impl, nil
}

// keyedSource is the snapshot of the source a check's key covers: the
// target's file set (see fileSet), widened by any stage inputs, less every
// declared output.
func keyedSource(req Request) snapshotRequest {
	paths := append([]string{}, req.Target.Inputs...)
	for _, stage := range req.stages() {
		paths = append(paths, stage.definition.Inputs...)
	}
	sort.Strings(paths)
	return snapshotRequest{
		Root:        req.Source,
		Paths:       paths,
		Excludes:    append(outputPaths(req), req.Target.Exclude...),
		Discovery:   req.Target.Discovery,
		GoToolchain: readsGoToolchain(req.Check.Kind),
	}
}

func fingerprint(ctx context.Context, req Request) (string, error) {
	source, err := sessionOf(req).snapshot(ctx, keyedSource(req))
	if err != nil {
		return "", err
	}
	impl, err := implementation(ctx, req)
	if err != nil {
		return "", err
	}

	req.RerunChecks = false
	var env []string
	toolchain := ""
	if req.Environment.Executor == ExecutorNative {
		env = nativeEnv(req, req.Check.env())
		toolchain, err = hostToolchain(ctx, req, env)
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
func hostToolchain(ctx context.Context, req Request, env []string) (string, error) {
	if !sharedGoCheck(req.Check.Kind) {
		return "", nil
	}

	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return "", err
	}
	identity, err := sessionOf(req).toolchainIdentity(ctx, dir, env)
	if err != nil {
		return "", err
	}
	return digest(identity), nil
}
