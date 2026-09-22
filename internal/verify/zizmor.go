package verify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// zizmorReleases is where upstream publishes zizmor's release archives. The
// Dagger path downloads from the same place; change both together.
const zizmorReleases = "https://github.com/zizmorcore/zizmor/releases/download"

// zizmorArchiveLimit bounds a download. The pinned archives are under 10 MB,
// so anything far larger is not the reviewed artifact.
const zizmorArchiveLimit = 64 << 20

// zizmorPin is the zizmor release workflow-security runs: a version and, per
// GOOS/GOARCH platform, the upstream archive and its reviewed SHA-256. It lives
// in the shared checkout's runner/toolchain.json so both executors run the same
// binary and the implementation snapshot covers it.
type zizmorPin struct {
	Version  string                   `json:"version"`
	Archives map[string]zizmorArchive `json:"archives"`
}

type zizmorArchive struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func readZizmorPin(shared string) (zizmorPin, error) {
	data, err := os.ReadFile(filepath.Join(shared, "runner", "toolchain.json"))
	if err != nil {
		return zizmorPin{}, fmt.Errorf("shared checkout has no runner/toolchain.json: %w", err)
	}

	var tools struct {
		Zizmor zizmorPin `json:"zizmor"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		return zizmorPin{}, err
	}
	if tools.Zizmor.Version == "" || len(tools.Zizmor.Archives) == 0 {
		return zizmorPin{}, fmt.Errorf("runner/toolchain.json pins no zizmor release")
	}
	return tools.Zizmor, nil
}

// archive is the pinned archive for one platform. A platform upstream does not
// publish, or one the pin has not reviewed, is an error rather than a fallback
// to an unverified build.
func (p zizmorPin) archive(platform string) (zizmorArchive, error) {
	archive, ok := p.Archives[platform]
	if !ok || archive.Name == "" || archive.Name != filepath.Base(archive.Name) || len(archive.SHA256) != sha256.Size*2 {
		return zizmorArchive{}, fmt.Errorf("workflow-security has no reviewed zizmor %s archive for %s", p.Version, platform)
	}
	return archive, nil
}

// installZizmor places the pinned zizmor binary for this host under
// work.Root/tools and returns its path. The archive is kept beside it and
// hashed on every run, so neither a partial download nor a changed cache file
// is ever executed: only bytes that match the reviewed SHA-256 are extracted.
func installZizmor(ctx context.Context, shared, root, releases string) (string, error) {
	pin, err := readZizmorPin(shared)
	if err != nil {
		return "", err
	}
	archive, err := pin.archive(runtime.GOOS + "/" + runtime.GOARCH)
	if err != nil {
		return "", err
	}

	dir := filepath.Join(root, "tools", "zizmor-"+pin.Version)
	unlock, err := lockFile(ctx, filepath.Join(root, "locks", "tool-zizmor"))
	if err != nil {
		return "", err
	}
	defer unlock()

	cached := filepath.Join(dir, archive.Name)
	data, err := os.ReadFile(cached)
	if err != nil || !matchesSHA256(data, archive.SHA256) {
		url := fmt.Sprintf("%s/v%s/%s", releases, pin.Version, archive.Name)
		data, err = download(ctx, url, zizmorArchiveLimit)
		if err != nil {
			return "", fmt.Errorf("downloading zizmor %s, which workflow-security needs network access to fetch once per cache directory: %w", pin.Version, err)
		}
		if !matchesSHA256(data, archive.SHA256) {
			return "", fmt.Errorf("zizmor archive %s does not match its reviewed SHA-256", archive.Name)
		}
		if err := atomicWrite(cached, data, 0600); err != nil {
			return "", err
		}
	}

	binary, err := untarFile(data, "zizmor")
	if err != nil {
		return "", fmt.Errorf("zizmor archive %s: %w", archive.Name, err)
	}
	path := filepath.Join(dir, "zizmor")
	if err := atomicWrite(path, binary, 0700); err != nil {
		return "", err
	}
	return path, nil
}

func matchesSHA256(data []byte, want string) bool {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == strings.ToLower(want)
}

// download reads at most limit bytes and refuses a larger body.
func download(ctx context.Context, url string, limit int) ([]byte, error) {
	child, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(child, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // Read errors are returned below.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, limit)
	}
	return data, nil
}

// untarFile returns one regular file from a gzipped tar archive.
func untarFile(data []byte, name string) ([]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s file", name)
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && filepath.Clean(header.Name) == name {
			return io.ReadAll(io.LimitReader(archive, zizmorArchiveLimit))
		}
	}
}

func (n *Native) workflowSecurity(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	inputs, config, err := zizmorInputs(req.Source, req.Target.Inputs, req.Target.Exclude)
	if err != nil {
		return nil, toolRun{}, err
	}
	binary, err := installZizmor(ctx, req.Shared, work.Root, zizmorReleases)
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, zizmorArguments(binary, config, inputs), zizmorEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := zizmorFindings(req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

// zizmorEnv drops what zizmor would otherwise read from the host: its own
// ZIZMOR_* settings, which could name another configuration or clash with the
// check's flags, and GitHub credentials, which an offline audit never needs.
// The container has none of them either.
func zizmorEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "ZIZMOR_") && name != "GH_TOKEN" && name != "GITHUB_TOKEN" && name != "GH_HOST" {
			out = append(out, entry)
		}
	}
	return out
}

// zizmorConfigs are the files zizmor would discover at a repository root, in
// its own order of precedence.
var zizmorConfigs = []string{".github/zizmor.yml", ".github/zizmor.yaml", "zizmor.yml", "zizmor.yaml"}

// zizmorInputs names every file workflow-security audits, the way the Dagger
// path does: the workflows, the composite actions at the root and under
// .github/actions, and the Dependabot configuration. zizmor's own discovery
// would read .gitignore and look for a configuration above the repository only
// when there is no .git, which an exported source never has, so the inputs and
// the configuration are both named explicitly and the executors agree. Only
// files under the target's declared inputs and outside its excludes count:
// they are all the Dagger path imports and all the fingerprint covers.
func zizmorInputs(source string, declaredInputs, excludes []string) ([]string, string, error) {
	visible := func(path string) bool {
		return declared(declaredInputs, path) && !excluded(path, excludes)
	}

	var inputs []string
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml", "action.yml", "action.yaml", ".github/dependabot.yml", ".github/dependabot.yaml"} {
		matches, err := filepath.Glob(filepath.Join(source, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, "", err
		}
		for _, match := range matches {
			if path := repositoryPath(source, match); visible(path) {
				inputs = append(inputs, filepath.ToSlash(path))
			}
		}
	}

	actions := filepath.Join(source, ".github", "actions")
	err := filepath.WalkDir(actions, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := repositoryPath(source, path)
		if !entry.IsDir() && (entry.Name() == "action.yml" || entry.Name() == "action.yaml") && visible(rel) {
			inputs = append(inputs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	if len(inputs) == 0 {
		return nil, "", fmt.Errorf("workflow-security found no workflows, composite actions or Dependabot configuration to audit")
	}

	var configs []string
	for _, config := range zizmorConfigs {
		info, err := os.Stat(filepath.Join(source, filepath.FromSlash(config)))
		if err == nil && info.Mode().IsRegular() && visible(filepath.FromSlash(config)) {
			configs = append(configs, config)
		}
	}
	if len(configs) > 1 {
		return nil, "", fmt.Errorf("configure only one zizmor file; found %s", strings.Join(configs, ", "))
	}

	sort.Strings(inputs)
	config := ""
	if len(configs) == 1 {
		config = configs[0]
	}
	return inputs, config, nil
}

// zizmorArguments audits offline, so the verdict depends only on the inputs,
// the configuration and the pinned binary, which is what makes it cacheable.
// Online audits stay with zizmor's own GitHub Action. Without a configuration
// file, --no-config keeps zizmor from finding one outside the repository.
// --strict-collection makes an unparsable input an error instead of a warning
// and a silent pass. runner/checks.go keeps a copy; change both together.
func zizmorArguments(binary, config string, inputs []string) []string {
	args := []string{binary, "--offline", "--strict-collection", "--min-severity=medium", "--format=plain", "--color=never", "--no-progress", "--quiet"}
	if config == "" {
		args = append(args, "--no-config")
	} else {
		args = append(args, "--config="+config)
	}
	return append(args, inputs...)
}

// zizmorFindings keeps zizmor's own report as the finding. zizmor exits 13 or
// 14 when its highest finding is medium or high; with --min-severity=medium
// nothing lower is reported, so 11 and 12 cannot mean findings here. 1 is an
// audit error, 2 a usage error and 3 no inputs; every other code is reserved.
// runner/checks.go keeps a copy; change both together.
func zizmorFindings(module string, exitCode int, stdout, stderr string) ([]finding, error) {
	if exitCode == 0 {
		return nil, nil
	}

	message := strings.TrimSpace(stdout + "\n" + stderr)
	if (exitCode != 13 && exitCode != 14) || message == "" {
		return nil, fmt.Errorf("zizmor exited %d: %s", exitCode, message)
	}
	return []finding{{
		Code:     string(CheckWorkflowSecurity),
		Message:  message,
		Location: location{File: module, Line: 1},
	}}, nil
}
