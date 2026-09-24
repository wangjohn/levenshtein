package main

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"dagger/levenshtein/internal/dagger"
)

// releasePin is one upstream release a shared check runs: where upstream
// publishes it, its version, and per OS/architecture platform the asset and
// its reviewed SHA-256. Binary is the binary's path inside a .tar.gz asset; an
// asset without it is the binary itself. internal/verify/releases.go keeps a
// copy; change both together.
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

// asset is the pinned asset for one platform, ignoring any CPU variant the
// engine reports. A platform upstream does not publish, or one the pin has not
// reviewed, is an error rather than a fallback to an unverified build.
func (p releasePin) asset(tool, platform string) (releaseAsset, error) {
	if parts := strings.Split(platform, "/"); len(parts) > 2 {
		platform = parts[0] + "/" + parts[1]
	}
	asset, ok := p.Assets[platform]
	if p.Releases == "" || p.Version == "" || !ok || asset.Name == "" || strings.Contains(asset.Name, "/") || len(asset.SHA256) != 64 {
		return releaseAsset{}, fmt.Errorf("no reviewed %s %s release asset for %s", tool, p.Version, platform)
	}
	if p.Binary != "" && (!strings.HasSuffix(asset.Name, ".tar.gz") || path.Clean(p.Binary) != p.Binary || strings.HasPrefix(p.Binary, "/") || strings.HasPrefix(p.Binary, "../")) {
		return releaseAsset{}, fmt.Errorf("%s %s must name a binary inside a .tar.gz asset", tool, p.Version)
	}
	return asset, nil
}

// withRelease installs the pinned binary for the container's own platform at
// dest. The engine fetches the asset with its reviewed checksum and refuses
// it unless the bytes match, so nothing unverified is ever extracted or run.
func withRelease(ctx context.Context, ctr *dagger.Container, pin releasePin, tool, dest string) (*dagger.Container, error) {
	platform, err := ctr.Platform(ctx)
	if err != nil {
		return nil, err
	}
	asset, err := pin.asset(tool, string(platform))
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/v%s/%s", strings.TrimSuffix(pin.Releases, "/"), pin.Version, asset.Name)
	download := dag.HTTP(url, dagger.HTTPOpts{Checksum: "sha256:" + asset.SHA256})

	if pin.Binary == "" {
		return ctr.WithFile(dest, download, dagger.ContainerWithFileOpts{Permissions: 0755}), nil
	}
	archive := "/tmp/" + asset.Name
	extracted := "/tmp/" + tool + "-release"
	strip := strconv.Itoa(strings.Count(pin.Binary, "/"))
	return ctr.WithMountedFile(archive, download).
		WithExec([]string{"mkdir", "-p", extracted}).
		WithExec([]string{"tar", "-xzf", archive, "-C", extracted, "--strip-components=" + strip, pin.Binary}).
		WithExec([]string{"install", "-m", "0755", path.Join(extracted, path.Base(pin.Binary)), dest}), nil
}
