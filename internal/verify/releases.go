package verify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// releaseLimit bounds a pinned release download and the binary taken from it.
// The largest pinned file, a ShellCheck binary for macOS, is about 60 MB, so
// anything far larger is not the reviewed artifact.
const releaseLimit = 128 << 20

// releaseTool names a pinned upstream release in runner/toolchain.json.
type releaseTool string

const (
	releaseShellCheck releaseTool = "shellcheck"
)

// releasePin is one upstream release a shared check runs on either executor:
// where upstream publishes it, its version, and per GOOS/GOARCH platform the
// asset and its reviewed SHA-256. Binary is the binary's path inside a .tar.gz
// asset; an asset without it is the binary itself. It lives in the shared
// checkout's runner/toolchain.json, beside zizmor's pin, so both executors run
// the same bytes and the implementation snapshot covers them.
// runner/releases.go keeps a copy; change both together.
type releasePin struct {
	Releases string                  `json:"releases"`
	Version  string                  `json:"version"`
	Binary   string                  `json:"binary"`
	Assets   map[string]releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func readReleasePin(shared string, tool releaseTool) (releasePin, error) {
	data, err := os.ReadFile(filepath.Join(shared, "runner", "toolchain.json"))
	if err != nil {
		return releasePin{}, fmt.Errorf("shared checkout has no runner/toolchain.json: %w", err)
	}

	var tools map[string]json.RawMessage
	if err := json.Unmarshal(data, &tools); err != nil {
		return releasePin{}, err
	}
	var pin releasePin
	if raw, ok := tools[string(tool)]; ok {
		if err := json.Unmarshal(raw, &pin); err != nil {
			return releasePin{}, fmt.Errorf("runner/toolchain.json %s: %w", tool, err)
		}
	}
	if pin.Releases == "" || pin.Version == "" || len(pin.Assets) == 0 {
		return releasePin{}, fmt.Errorf("runner/toolchain.json pins no %s release", tool)
	}
	if pin.Binary != "" && (!filepath.IsLocal(pin.Binary) || path.Clean(pin.Binary) != pin.Binary) {
		return releasePin{}, fmt.Errorf("runner/toolchain.json %s binary %q is not a path inside its archive", tool, pin.Binary)
	}
	return pin, nil
}

// asset is the pinned asset for one platform. A platform upstream does not
// publish, or one the pin has not reviewed, is an error rather than a fallback
// to an unverified build.
func (p releasePin) asset(tool releaseTool, platform string) (releaseAsset, error) {
	asset, ok := p.Assets[platform]
	if !ok || asset.Name == "" || asset.Name != filepath.Base(asset.Name) || len(asset.SHA256) != sha256.Size*2 {
		return releaseAsset{}, fmt.Errorf("no reviewed %s %s release asset for %s", tool, p.Version, platform)
	}
	if p.Binary != "" && !strings.HasSuffix(asset.Name, ".tar.gz") {
		return releaseAsset{}, fmt.Errorf("%s %s asset %s must be a .tar.gz archive", tool, p.Version, asset.Name)
	}
	return asset, nil
}

// installRelease places the pinned binary for this host under root/tools and
// returns its path, the way installZizmor does for zizmor. The downloaded
// asset is kept beside it and hashed on every run, so neither a partial
// download nor a changed cache file is ever executed: only bytes that match the
// reviewed SHA-256 are extracted or copied. releases overrides the pin's
// download location, for tests; empty means the pin's own.
func installRelease(ctx context.Context, shared, root string, tool releaseTool, releases string) (string, error) {
	pin, err := readReleasePin(shared, tool)
	if err != nil {
		return "", err
	}
	asset, err := pin.asset(tool, runtime.GOOS+"/"+runtime.GOARCH)
	if err != nil {
		return "", err
	}
	if releases == "" {
		releases = pin.Releases
	}

	dir := filepath.Join(root, "tools", string(tool)+"-"+pin.Version)
	unlock, err := lockFile(ctx, filepath.Join(root, "locks", "tool-"+string(tool)))
	if err != nil {
		return "", err
	}
	defer unlock()

	cached := filepath.Join(dir, asset.Name)
	data, err := os.ReadFile(cached)
	if err != nil || !matchesSHA256(data, asset.SHA256) {
		url := fmt.Sprintf("%s/v%s/%s", strings.TrimSuffix(releases, "/"), pin.Version, asset.Name)
		data, err = download(ctx, url, releaseLimit)
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
	if err := atomicWrite(installed, binary, 0700); err != nil {
		return "", err
	}
	return installed, nil
}

// untarEntry returns one regular file from a gzipped tar archive, refusing one
// larger than limit rather than truncating it.
func untarEntry(data []byte, name string, limit int64) ([]byte, error) {
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
		if header.Typeflag != tar.TypeReg || path.Clean(header.Name) != name {
			continue
		}
		if header.Size > limit {
			return nil, fmt.Errorf("%s is larger than %d bytes", name, limit)
		}
		return io.ReadAll(io.LimitReader(archive, header.Size))
	}
}
