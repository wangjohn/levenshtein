package verify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// This mirrors the runner's workflow-security test: only zizmor's medium and
// high finding codes are findings, and every other nonzero exit is a tool
// error, never a pass.
func TestZizmorFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	for _, tc := range []struct {
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{code: 14, stdout: "error[template-injection]: code injection via template expansion"},
		{code: 13, stdout: "warning[excessive-permissions]: overly broad permissions"},
		{code: 1, stderr: "fatal: no audit was performed", wantError: true},
		{code: 2, stderr: "error: unexpected argument '--bogus' found", wantError: true},
		{code: 3, stderr: "fatal: no inputs collected", wantError: true},
		{code: 12, stdout: "help[self-repository]: use GitHub's dedicated self-repository syntax", wantError: true},
		{code: 11, stdout: "info[template-injection]: code injection via template expansion", wantError: true},
		{code: 14, wantError: true},
		{code: 137, stderr: "killed", wantError: true},
	} {
		findings, err := zizmorFindings(".", tc.code, tc.stdout, tc.stderr)
		if (err != nil) != tc.wantError {
			t.Fatalf("exit %d: findings=%v error=%v", tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Code != string(CheckWorkflowSecurity) || findings[0].Message != strings.TrimSpace(tc.stdout+"\n"+tc.stderr) || findings[0].Location.File != ".") {
			t.Fatalf("lost zizmor's report: %v", findings)
		}
	}

	if findings, err := zizmorFindings(".", 0, "No findings to report. Good job!", ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
}

func TestZizmorInputsNameWorkflowsActionsAndDependabot(t *testing.T) {
	source := t.TempDir()
	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yaml",
		".github/workflows/notes.md",
		".github/actions/setup/action.yml",
		".github/actions/nested/deeper/action.yaml",
		".github/actions/setup/README.md",
		".github/dependabot.yml",
		"action.yml",
		"vendor/some/action.yml",
		"runner/testdata/bad/.github/workflows/bad.yml",
	} {
		writeTestFile(t, filepath.Join(source, filepath.FromSlash(path)), "name: x\n")
	}

	inputs, config, err := zizmorInputs(source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/actions/nested/deeper/action.yaml", ".github/actions/setup/action.yml", ".github/dependabot.yml", ".github/workflows/ci.yml", ".github/workflows/release.yaml", "action.yml"}
	if !slices.Equal(inputs, want) || config != "" {
		t.Fatalf("inputs=%v config=%q, want %v and no configuration", inputs, config, want)
	}
	if args := zizmorArguments("zizmor", config, inputs); !slices.Contains(args, "--no-config") || !slices.Contains(args, "--offline") {
		t.Fatalf("an unconfigured audit must stay offline and ignore configurations outside the repository: %v", args)
	}

	writeTestFile(t, filepath.Join(source, ".github", "zizmor.yml"), "rules: {}\n")
	if _, config, err := zizmorInputs(source); err != nil || config != ".github/zizmor.yml" {
		t.Fatalf("the repository's zizmor configuration was not used: %q %v", config, err)
	}
	if args := zizmorArguments("zizmor", ".github/zizmor.yml", inputs); !slices.Contains(args, "--config=.github/zizmor.yml") || slices.Contains(args, "--no-config") {
		t.Fatalf("a configured audit must name its file: %v", args)
	}

	writeTestFile(t, filepath.Join(source, "zizmor.yaml"), "rules: {}\n")
	if _, _, err := zizmorInputs(source); err == nil || !strings.Contains(err.Error(), "configure only one zizmor file") {
		t.Fatalf("two configurations must be an error: %v", err)
	}
}

func TestZizmorInputsRefuseAnEmptyAudit(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "README.md"), "nothing to audit\n")

	if _, _, err := zizmorInputs(source); err == nil {
		t.Fatal("a repository with nothing to audit must be an error, not a pass")
	}
}

// Host settings must not change what the audit reads or how it runs.
func TestZizmorEnvDropsHostSettingsAndCredentials(t *testing.T) {
	env := zizmorEnv([]string{"PATH=/bin", "ZIZMOR_CONFIG=/elsewhere.yml", "ZIZMOR_OFFLINE=false", "GH_TOKEN=secret", "GITHUB_TOKEN=secret", "GH_HOST=example.com", "HOME=/home/user"})
	if !slices.Equal(env, []string{"PATH=/bin", "HOME=/home/user"}) {
		t.Fatalf("unexpected zizmor environment: %v", env)
	}
}

// installZizmor runs only bytes that match the reviewed checksum, downloads
// them once per cache, and recovers from a changed cache file.
func TestInstallZizmorVerifiesTheArchiveBeforeExtracting(t *testing.T) {
	binary := []byte("#!/bin/sh\necho zizmor 1.30.1\n")
	archive := tarGz(t, "zizmor", binary)
	var requests atomic.Int32
	var served atomic.Pointer[[]byte]
	served.Store(&archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1.30.1/zizmor-test.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(*served.Load())
	}))
	defer server.Close()

	shared := t.TempDir()
	sum := sha256.Sum256(archive)
	pin := map[string]any{"zizmor": zizmorPin{
		Version:  "1.30.1",
		Archives: map[string]zizmorArchive{runtime.GOOS + "/" + runtime.GOARCH: {Name: "zizmor-test.tar.gz", SHA256: hex.EncodeToString(sum[:])}},
	}}
	data, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(shared, "runner", "toolchain.json"), string(data))
	root := t.TempDir()
	ctx := context.Background()

	path, err := installZizmor(ctx, shared, root, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("installed %q, %v", got, err)
	}
	if _, err := installZizmor(ctx, shared, root, server.URL); err != nil || requests.Load() != 1 {
		t.Fatalf("a verified archive must be reused: %d requests, %v", requests.Load(), err)
	}

	cached := filepath.Join(root, "tools", "zizmor-1.30.1", "zizmor-test.tar.gz")
	writeTestFile(t, cached, "tampered")
	if _, err := installZizmor(ctx, shared, root, server.URL); err != nil || requests.Load() != 2 {
		t.Fatalf("a changed cache file must be downloaded again: %d requests, %v", requests.Load(), err)
	}

	substituted := tarGz(t, "zizmor", []byte("#!/bin/sh\necho substituted\n"))
	served.Store(&substituted)
	fresh := t.TempDir()
	if _, err := installZizmor(ctx, shared, fresh, server.URL); err == nil || !strings.Contains(err.Error(), "does not match its reviewed SHA-256") {
		t.Fatalf("a substituted archive must be refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "tools", "zizmor-1.30.1", "zizmor")); !os.IsNotExist(err) {
		t.Fatalf("nothing from a refused archive may be installed: %v", err)
	}
}

func TestInstallZizmorRefusesAnUnpinnedPlatform(t *testing.T) {
	shared := t.TempDir()
	writeTestFile(t, filepath.Join(shared, "runner", "toolchain.json"), `{"zizmor":{"version":"1.30.1","archives":{"plan9/mips":{"name":"zizmor.tar.gz","sha256":"`+strings.Repeat("0", 64)+`"}}}}`)

	_, err := installZizmor(context.Background(), shared, t.TempDir(), "http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "no reviewed zizmor 1.30.1 archive for "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("an unpinned platform must be an error, not an unverified download: %v", err)
	}
}

// The shared checkout pins every platform the native executor runs on, and
// zizmor's own GitHub Action runs the same release for the online audits.
func TestZizmorPinCoversNativePlatformsAndMatchesTheAction(t *testing.T) {
	pin, err := readZizmorPin("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
		if _, err := pin.archive(platform); err != nil {
			t.Error(err)
		}
	}

	workflow, err := os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)zizmorcore/zizmor-action@.*?\n\s+version: (\S+)`).FindSubmatch(workflow)
	if match == nil || string(match[1]) != pin.Version {
		t.Fatalf("security.yml must run zizmor %s like runner/toolchain.json; found %q", pin.Version, match)
	}
}

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	compressed := gzip.NewWriter(&out)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
