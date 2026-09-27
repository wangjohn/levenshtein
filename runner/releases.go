package main

import (
	"context"
	"path"
	"strconv"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"
)

// withRelease installs the pinned binary for the container's own platform at
// dest. The engine fetches the asset with its reviewed checksum and refuses
// it unless the bytes match, so nothing unverified is ever extracted or run.
func withRelease(ctx context.Context, ctr *dagger.Container, pin checktool.ReleasePin, tool, dest string) (*dagger.Container, error) {
	platform, err := ctr.Platform(ctx)
	if err != nil {
		return nil, err
	}
	asset, err := pin.Asset(tool, string(platform))
	if err != nil {
		return nil, err
	}
	download := dag.HTTP(pin.AssetURL(asset, ""), dagger.HTTPOpts{Checksum: "sha256:" + asset.SHA256})

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
