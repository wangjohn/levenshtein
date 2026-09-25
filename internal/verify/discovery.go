package verify

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A default target declares inputs: ["."], so a filesystem walk hashes every
// build artifact, dependency directory and editor scratch file in the tree. The
// work tree already knows which files belong to the repository, so ask it once
// per run instead.
//
// The trade-off this records: a file the repository's .gitignore files ignore
// is not part of the fingerprint, and so is not part of what the check reads
// either: the Dagger import leaves out the ignored paths and the native
// scanners read the listing. A target whose real inputs are generated and
// gitignored must declare "discovery": "filesystem". The Go kinds add the
// ignored paths their toolchain can load (see goLoader), because it reads them
// whatever the listing says.
type gitListing struct {
	// files are source-relative paths, sorted, of everything git tracks or would
	// add. Nil means the listing is unusable and callers walk the filesystem.
	files []string
	// ignored are the source-relative paths, sorted, of the untracked files the
	// repository's .gitignore files ignore, a wholly ignored directory named
	// once. Everything on disk is in files or in ignored, so excluding ignored
	// from a directory import leaves exactly files.
	ignored []string
	// goDirectories memoizes goLoader's scan of each ignored directory for as
	// long as this listing lives, so a run reads a large ignored tree's names
	// once, and again only after relist.
	goDirectories sync.Map
	// note explains an unexpected failure for the cache Reason. A directory that
	// is simply not a work tree is ordinary and carries no note.
	note string
}

// listing is source's git listing, memoized for the life of the session,
// matching the run-scoped view the implementation snapshot already takes.
func (s *Session) listing(ctx context.Context, source string) *gitListing {
	if memoized, ok := s.listings.Load(source); ok {
		return memoized.(*gitListing)
	}

	s.listingMu.Lock()
	epoch := s.listingEpochs[source]
	s.listingMu.Unlock()

	found, err := listGit(ctx, source)
	if hook := s.listingFetched; hook != nil {
		hook(source)
	}
	if err != nil {
		// A cancelled or failed git run says nothing about the work tree, so it
		// is not memoized: the next snapshot asks git again rather than walking
		// the filesystem for the rest of the run.
		return &gitListing{note: "git input discovery failed, so inputs were enumerated from the filesystem: " + err.Error()}
	}

	// A check that created files relisted while git ran, so this view may
	// predate them. It serves this caller, but is not the run's.
	s.listingMu.Lock()
	defer s.listingMu.Unlock()
	if s.listingEpochs[source] != epoch {
		return found
	}
	memoized, _ := s.listings.LoadOrStore(source, found)
	return memoized.(*gitListing)
}

// gitFiles reports the work tree's files under source, or false when the caller
// must walk the filesystem instead.
func (s *Session) gitFiles(ctx context.Context, source string) ([]string, bool) {
	found := s.listing(ctx, source)
	return found.files, found.files != nil
}

// relist drops source's memoized listing so the next snapshot asks git again.
// A check can create files, and a result must not be cached under a key that
// could not see them.
func (s *Session) relist(source string) {
	s.listingMu.Lock()
	defer s.listingMu.Unlock()

	s.listingEpochs[source]++
	s.listings.Delete(source)
}

// discoveryNote explains why a git-discovery target fell back to a filesystem
// walk, for the result's cache Reason. It is empty in the ordinary cases.
func (s *Session) discoveryNote(ctx context.Context, source string, mode DiscoveryKind) string {
	if mode != DiscoveryGit {
		return ""
	}
	return s.listing(ctx, source).note
}

// hostGit finds git on the process PATH, considering absolute entries only. A
// relative entry such as "." would resolve inside the repository being
// verified, and discovery must never run a git that repository ships.
func hostGit() (string, bool) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "git")
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

// listGit runs git itself: the index and the ignore rules are git's, and
// reimplementing them would disagree with the repository in exactly the cases
// that matter. The child gets PATH and HOME only, so committed configuration
// cannot choose which git runs.
//
// Only the repository's own .gitignore files apply. The standard exclusions
// would also honour the developer's core.excludesFile and .git/info/exclude, so
// an untracked input one person ignores privately would leave their fingerprint
// and not a colleague's; core.excludesFile is also cleared explicitly.
//
// A directory outside any work tree, or a host without git, is an ordinary
// answer. An error means git could not answer this time, including when ctx
// ends first.
func listGit(ctx context.Context, source string) (*gitListing, error) {
	env := []string{}
	for _, name := range []string{"PATH", "HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	path, ok := hostGit()
	if !ok {
		return &gitListing{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	files, outside, err := lsFiles(ctx, path, source, env, "--cached", "--others")
	if err != nil {
		return nil, err
	}
	if outside {
		return &gitListing{}, nil
	}
	// An empty listing under a work tree means the source itself is ignored.
	// Fingerprinting almost nothing would make unrelated trees look identical,
	// so walk the filesystem and say why.
	if len(files) == 0 {
		return &gitListing{note: "git lists no files under the source, so inputs were enumerated from the filesystem"}, nil
	}
	// The same rules, asked the other way round: what the listing leaves out.
	// --directory names a wholly ignored directory once, so a dependency tree
	// costs one entry, not one per file.
	ignored, _, err := lsFiles(ctx, path, source, env, "--others", "--ignored", "--directory")
	if err != nil {
		return nil, err
	}
	return &gitListing{files: files, ignored: ignored}, nil
}

// lsFiles runs git ls-files with the repository's own .gitignore rules and
// returns the paths it prints, source-relative and sorted. outside reports a
// source that is not in a work tree.
func lsFiles(ctx context.Context, git, source string, env []string, args ...string) (paths []string, outside bool, err error) {
	cmd := exec.CommandContext(ctx, git, append([]string{"-c", "core.excludesFile=", "ls-files", "-z", "--exclude-per-directory=.gitignore"}, args...)...)
	cmd.Dir = source
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "not a git repository") {
			return nil, true, nil
		}
		return nil, false, errors.New(strings.TrimSpace(stderr.String() + " " + err.Error()))
	}

	for name := range strings.SplitSeq(stdout.String(), "\x00") {
		// A directory is listed with a trailing slash: an untracked nested
		// repository, which the enumeration walks into, or a wholly ignored one.
		path := filepath.FromSlash(strings.TrimSuffix(name, "/"))
		if name == "" || !relative(path) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, false, nil
}
