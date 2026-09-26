package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// dir: the clone's .git directory, whichever of its worktrees dir is in.
func gitCommonDir(ctx context.Context, dir string) (string, bool) {
	git, ok := hostGit()
	if !ok {
		return "", false
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	cmd.Env = hostGitEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}

	common := strings.TrimSpace(stdout.String())
	if !filepath.IsAbs(common) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return "", false
	}
	return resolved, true
}
