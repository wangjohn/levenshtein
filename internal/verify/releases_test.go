package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// writeReleasePin writes a toolchain.json into a fresh shared checkout that
// pins one asset for this host.
func writeReleasePin(t *testing.T, tool releaseTool, binary, name string, asset []byte) string {
	t.Helper()
	sum := sha256.Sum256(asset)
	pin := map[string]releasePin{string(tool): {
		Releases: "https://example.invalid/releases",
		Version:  "1.2.3",
		Binary:   binary,
		Assets:   map[string]releaseAsset{runtime.GOOS + "/" + runtime.GOARCH: {Name: name, SHA256: hex.EncodeToString(sum[:])}},
	}}
	data, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}

	shared := t.TempDir()
	writeTestFile(t, filepath.Join(shared, "runner", "toolchain.json"), string(data))
	return shared
}

// installRelease runs only bytes that match the reviewed checksum, downloads
// them once per cache, recovers from a changed cache file, and takes the
// binary from inside an archive or uses the asset itself.
func TestInstallReleaseVerifiesTheAssetBeforeUsingIt(t *testing.T) {
	binary := []byte("#!/bin/sh\necho shellcheck 1.2.3\n")
	archive := tarGz(t, "shellcheck-v1.2.3/shellcheck", binary)
	var requests atomic.Int32
	var served atomic.Pointer[[]byte]
	served.Store(&archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1.2.3/shellcheck.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(*served.Load())
	}))
	defer server.Close()

	shared := writeReleasePin(t, releaseShellCheck, "shellcheck-v1.2.3/shellcheck", "shellcheck.tar.gz", archive)
	root := t.TempDir()
	ctx := context.Background()

	path, err := installRelease(ctx, shared, root, releaseShellCheck, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("installed %q, %v", got, err)
	}
	if _, err := installRelease(ctx, shared, root, releaseShellCheck, server.URL); err != nil || requests.Load() != 1 {
		t.Fatalf("a verified asset must be reused: %d requests, %v", requests.Load(), err)
	}

	writeTestFile(t, filepath.Join(root, "tools", "shellcheck-1.2.3", "shellcheck.tar.gz"), "tampered")
	if _, err := installRelease(ctx, shared, root, releaseShellCheck, server.URL); err != nil || requests.Load() != 2 {
		t.Fatalf("a changed cache file must be downloaded again: %d requests, %v", requests.Load(), err)
	}

	substituted := tarGz(t, "shellcheck-v1.2.3/shellcheck", []byte("#!/bin/sh\necho substituted\n"))
	served.Store(&substituted)
	fresh := t.TempDir()
	if _, err := installRelease(ctx, shared, fresh, releaseShellCheck, server.URL); err == nil || !strings.Contains(err.Error(), "does not match its reviewed SHA-256") {
		t.Fatalf("a substituted asset must be refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "tools", "shellcheck-1.2.3", "shellcheck")); !os.IsNotExist(err) {
		t.Fatalf("nothing from a refused asset may be installed: %v", err)
	}
}

// releaseExample is a tool no toolchain.json pins, for tests that write their
// own pin.
const releaseExample releaseTool = "example"

func TestInstallReleaseUsesABinaryAssetAsIs(t *testing.T) {
	binary := []byte("#!/bin/sh\necho example 1.2.3\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(binary)
	}))
	defer server.Close()
	shared := writeReleasePin(t, releaseExample, "", "example_linux", binary)

	path, err := installRelease(context.Background(), shared, t.TempDir(), releaseExample, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0100 == 0 {
		t.Fatalf("the binary must be installed executable: %v %v", info, err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("installed %q, %v", got, err)
	}
}

func TestInstallReleaseRefusesAnUnpinnedPlatform(t *testing.T) {
	shared := t.TempDir()
	writeTestFile(t, filepath.Join(shared, "runner", "toolchain.json"), `{"shellcheck":{"releases":"https://example.invalid","version":"1.2.3","binary":"shellcheck","assets":{"plan9/mips":{"name":"shellcheck.tar.gz","sha256":"`+strings.Repeat("0", 64)+`"}}}}`)

	_, err := installRelease(context.Background(), shared, t.TempDir(), releaseShellCheck, "http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "no reviewed shellcheck 1.2.3 release asset for "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("an unpinned platform must be an error, not an unverified download: %v", err)
	}
}

func TestReadReleasePinRefusesAPathOutsideTheArchive(t *testing.T) {
	shared := t.TempDir()
	writeTestFile(t, filepath.Join(shared, "runner", "toolchain.json"), `{"shellcheck":{"releases":"https://example.invalid","version":"1.2.3","binary":"../shellcheck","assets":{"linux/amd64":{"name":"shellcheck.tar.gz","sha256":"`+strings.Repeat("0", 64)+`"}}}}`)

	if _, err := readReleasePin(shared, releaseShellCheck); err == nil {
		t.Fatal("a binary path outside the archive must be refused")
	}
}

// An archive entry larger than the limit is refused rather than truncated.
func TestUntarEntryRefusesAnEntryOverTheLimit(t *testing.T) {
	archive := tarGz(t, "dir/tool", []byte("0123456789"))

	if got, err := untarEntry(archive, "dir/tool", 10); err != nil || string(got) != "0123456789" {
		t.Fatalf("an entry at the limit must be returned whole: %q, %v", got, err)
	}
	if _, err := untarEntry(archive, "dir/tool", 9); err == nil {
		t.Fatal("an entry over the limit must be refused")
	}
	if _, err := untarEntry(archive, "tool", 10); err == nil {
		t.Fatal("a missing entry must be an error")
	}
}

// The shared checkout pins every platform the native executor runs on.
func TestReleasePinsCoverNativePlatforms(t *testing.T) {
	for _, tool := range []releaseTool{releaseShellCheck, releaseOSVScanner} {
		pin, err := readReleasePin("../..", tool)
		if err != nil {
			t.Fatal(err)
		}
		for _, platform := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
			if _, err := pin.asset(tool, platform); err != nil {
				t.Error(err)
			}
		}
	}
}
