package verify

import (
	"bytes"
	"context"
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
// is not part of the fingerprint, so a target whose real inputs are generated
// and gitignored must declare "discovery": "filesystem".
type gitListing struct {
	// files are source-relative paths, sorted, of everything git tracks or would
	// add. Nil means the listing is unusable and callers walk the filesystem.
	files []string
	// note explains an unexpected failure for the cache Reason. A directory that
	// is simply not a work tree is ordinary and carries no note.
	note string
}

// gitListings memoizes one listing per source for the life of the process,
// matching the run-scoped view the implementation snapshot already takes.
var gitListings sync.Map

func listing(source string) *gitListing {
	if memoized, ok := gitListings.Load(source); ok {
		return memoized.(*gitListing)
	}

	found := listGit(source)
	memoized, _ := gitListings.LoadOrStore(source, found)
	return memoized.(*gitListing)
}

// gitFiles reports the work tree's files under source, or false when the caller
// must walk the filesystem instead.
func gitFiles(source string) ([]string, bool) {
	found := listing(source)
	return found.files, found.files != nil
}

// relist drops source's memoized listing so the next snapshot asks git again.
// A check can create files, and a result must not be cached under a key that
// could not see them.
func relist(source string) {
	gitListings.Delete(source)
}

// discoveryNote explains why a git-discovery target fell back to a filesystem
// walk, for the result's cache Reason. It is empty in the ordinary cases.
func discoveryNote(source string, mode DiscoveryKind) string {
	if mode != DiscoveryGit {
		return ""
	}
	return listing(source).note
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
func listGit(source string) *gitListing {
	env := []string{}
	for _, name := range []string{"PATH", "HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	path, ok := hostGit()
	if !ok {
		return &gitListing{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-c", "core.excludesFile=", "ls-files", "-z", "--cached", "--others", "--exclude-per-directory=.gitignore")
	cmd.Dir = source
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "not a git repository") {
			return &gitListing{}
		}
		return &gitListing{note: "git input discovery failed, so inputs were enumerated from the filesystem: " + strings.TrimSpace(stderr.String()+" "+err.Error())}
	}

	var files []string
	for name := range strings.SplitSeq(stdout.String(), "\x00") {
		// An untracked nested repository is listed as its directory with a
		// trailing slash; keep it so the snapshot walks into it.
		path := filepath.FromSlash(strings.TrimSuffix(name, "/"))
		if name == "" || !relative(path) {
			continue
		}
		files = append(files, path)
	}
	// An empty listing under a work tree means the source itself is ignored.
	// Fingerprinting almost nothing would make unrelated trees look identical,
	// so walk the filesystem and say why.
	if len(files) == 0 {
		return &gitListing{note: "git lists no files under the source, so inputs were enumerated from the filesystem"}
	}

	sort.Strings(files)
	return &gitListing{files: files}
}
