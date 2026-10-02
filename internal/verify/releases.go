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
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// releaseLimit bounds a pinned release download and the binary taken from it.
// The largest pinned file, a ShellCheck binary for macOS, is about 60 MB, so
// anything far larger is not the reviewed artifact.
const releaseLimit = 128 << 20

// releaseTool names a pinned upstream release in runner/toolchain.json.
type releaseTool string

const (
	releaseShellCheck releaseTool = "shellcheck"
	releaseOSVScanner releaseTool = "osvScanner"
	releaseZizmor     releaseTool = "zizmor"
)

// readReleasePin reads one pin from the shared checkout's
// runner/toolchain.json, which the runner embeds, so both executors run the
// same bytes and the implementation snapshot covers them.
func readReleasePin(shared string, tool releaseTool) (checktool.ReleasePin, error) {
	data, err := os.ReadFile(filepath.Join(shared, "runner", "toolchain.json"))
	if err != nil {
		return checktool.ReleasePin{}, fmt.Errorf("shared checkout has no runner/toolchain.json: %w", err)
	}

	var tools map[string]json.RawMessage
	if err := json.Unmarshal(data, &tools); err != nil {
		return checktool.ReleasePin{}, err
	}
	var pin checktool.ReleasePin
	if raw, ok := tools[string(tool)]; ok {
		if err := json.Unmarshal(raw, &pin); err != nil {
			return checktool.ReleasePin{}, fmt.Errorf("runner/toolchain.json %s: %w", tool, err)
		}
	}
	if err := pin.Check(string(tool)); err != nil {
		return checktool.ReleasePin{}, err
	}
	return pin, nil
}

// installRelease places the pinned binary for this host under root/tools and
// returns its path. The downloaded
// asset is kept beside it and hashed on every run, so neither a partial
// download nor a changed cache file is ever executed: only bytes that match the
// reviewed SHA-256 are extracted or copied. releases overrides the pin's
// download location, for tests; empty means the pin's own.
func installRelease(ctx context.Context, shared, root string, tool releaseTool, releases string) (string, error) {
	pin, err := readReleasePin(shared, tool)
	if err != nil {
		return "", err
	}
	asset, err := pin.Asset(string(tool), runtime.GOOS+"/"+runtime.GOARCH)
	if err != nil {
		return "", err
	}

	dir := filepath.Join(root, "tools", string(tool)+"-"+pin.Version)
	unlock, err := lockFile(ctx, filepath.Join(root, "locks", "tool-"+string(tool)), nil)
	if err != nil {
		return "", err
	}
	defer unlock()

	cached := filepath.Join(dir, asset.Name)
	data, err := os.ReadFile(cached)
	if err != nil || !matchesSHA256(data, asset.SHA256) {
		data, err = download(ctx, pin.AssetURL(asset, releases), releaseLimit)
		if err != nil {
			return "", fmt.Errorf("downloading %s %s, which needs network access to fetch once per cache directory: %w", tool, pin.Version, err)
		}
		if !matchesSHA256(data, asset.SHA256) {
			return "", fmt.Errorf("%s asset %s does not match its reviewed SHA-256", tool, asset.Name)
		}
		if err := atomicWrite(cached, data, 0600); err != nil {
			return "", err
		}
	}

	binary := data
	if pin.Binary != "" {
		binary, err = untarEntry(data, pin.Binary, releaseLimit)
		if err != nil {
			return "", fmt.Errorf("%s archive %s: %w", tool, asset.Name, err)
		}
	}
	installed := filepath.Join(dir, string(tool))
	if err := atomicWriteExecutable(installed, binary); err != nil {
		return "", err
	}
	return installed, nil
}

// untarEntry returns one regular file from a gzipped tar archive, refusing one
// larger than limit rather than truncating it. Traversal, including skipped
// entries, reads at most releaseLimit decompressed bytes.
func untarEntry(data []byte, name string, limit int64) ([]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = compressed.Close() }() // The gzip reader owns no file handle.
	archive := tar.NewReader(io.LimitReader(compressed, releaseLimit))
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s file", name)
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg || path.Clean(header.Name) != name {
			continue
		}
		if header.Size > limit {
			return nil, fmt.Errorf("%s is larger than %d bytes", name, limit)
		}
		return io.ReadAll(io.LimitReader(archive, header.Size))
	}
}

// matchesSHA256 reports whether data is the file whose digest was reviewed.
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
