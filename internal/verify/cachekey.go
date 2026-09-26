package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// repositoryKey names the clone source belongs to, for the Dagger functions
// that run a repository's own code: the runner keeps their Go module and build
// cache volumes per key, so one repository's tests cannot change what
// another's compile on a persistent engine.
//
// The key is the SHA-256 of the clone's git common directory, absolute and
// with symlinks resolved, so every worktree of one clone shares it, and with
// it warm caches, while separate clones differ. Outside a work tree, or when
// git cannot answer, it is the SHA-256 of the resolved source root instead,
// which only ever separates more. The repository cannot choose it: git runs
// the way input discovery runs it (see listGit), with neither a committed
// file nor the environment able to redirect it.
//
// The key is not part of a check's result fingerprint. It picks where Go keeps
// reusable work, not what the verdict depends on, and keying results by it
// would stop separate clones of one commit from sharing a verdict.
func repositoryKey(ctx context.Context, source string) string {
	root, err := filepath.Abs(source)
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
	}
	identity := root
	if common, ok := gitCommonDir(ctx, root); ok {
		identity = common
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

// gitCommonDir is the resolved git common directory of the work tree holding
// dir: the clone's .git directory, whichever of its worktrees dir is in. It
// answers only when the work tree's .git is the clone's own git directory, or
// is a linked worktree's .git file whose git directory, under the common
// directory's worktrees, links back to it. A .git file or a commondir file can
// otherwise name any directory, so a directory that is not a clone, such as an
// unpacked archive, could claim another clone's key. A submodule's or a
// --separate-git-dir clone's .git file is refused the same way and falls back
// to the source root, which only ever separates more.
func gitCommonDir(ctx context.Context, dir string) (string, bool) {
	git, ok := hostGit()
	if !ok {
		return "", false
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	cmd.Dir = dir
	cmd.Env = hostGitEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		return "", false
	}
	resolved := make([]string, 0, len(lines))
	for _, line := range lines {
		if !filepath.IsAbs(line) {
			return "", false
		}
		path, err := filepath.EvalSymlinks(line)
		if err != nil {
			return "", false
		}
		resolved = append(resolved, path)
	}
	top, gitDir, common := resolved[0], resolved[1], resolved[2]
	if !ownsGitDir(top, gitDir, common) {
		return "", false
	}
	return common, true
}

// ownsGitDir reports whether the work tree at top is the one gitDir and
// common belong to: an ordinary clone whose .git directory is both, or a
// linked worktree whose .git file names a git directory under the common
// directory's worktrees that names that .git file back.
func ownsGitDir(top, gitDir, common string) bool {
	dotGit := filepath.Join(top, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return gitDir == dotGit && common == dotGit
	}
	if !info.Mode().IsRegular() || filepath.Dir(gitDir) != filepath.Join(common, "worktrees") {
		return false
	}
	link, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		return false
	}
	back, err := filepath.EvalSymlinks(strings.TrimSpace(string(link)))
	return err == nil && back == dotGit
}
