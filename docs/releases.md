# Release archives

GoReleaser builds the CLI for macOS and Linux on amd64 and arm64, and creates archives with SHA-256 checksums. Each archive includes the shared Dagger module, lint/tool modules, patched SDK adapter, fixtures, and the source files used to identify its implementation. Keep the archive together so the binary and checks have the same revision.

Using Go 1.27.1 and GoReleaser 2.18.1, validate and build locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Snapshot builds create files in `dist/` and publish nothing.

## Publishing a release

Publication is a tag. Push an annotated `vX.Y.Z` tag on a reviewed `main`
commit and `.github/workflows/release.yml` does the rest:

```sh
git tag -a v0.1.0 -m 'Levenshtein v0.1.0'
git push origin v0.1.0
```

The workflow checks out the full history, builds with the pinned Go from
`.go-version`, installs the pinned syft, and runs `goreleaser release --clean`.
The GitHub release then holds:

- the four platform archives (`linux`/`darwin` × `amd64`/`arm64`), each with the
  CLI and the shared check sources;
- an SPDX SBOM per archive;
- `checksums.txt` covering every published file;
- a build provenance attestation for the archives and the checksum file, which
  `gh attestation verify <file> --repo wangjohn/levenshtein` checks.

Nothing else is automated: the tag is created by a person, and a release is only
as reviewed as the commit it points at. `release-smoke` in the self-checks
workflow validates `.goreleaser.yaml` and builds the same archives as a snapshot
on every ready pull request, so a tag is not the first time the configuration
runs.

## Pinning a release

A consumer may pin a published tag instead of a commit SHA. A tag is readable in
a diff and is what release notes name; a SHA cannot be moved. Either is a
deliberate, reviewed update. Do not pin a branch.

After extracting an archive, run the binary with explicit paths:

```sh
/path/to/archive/levenshtein pre-merge --shared /path/to/archive --source /path/to/app
```

The binary needs no host Go compiler. Go lint still needs a Docker-compatible runtime; the SDK downloads the pinned Dagger CLI when needed. Native checks require the tools declared by their configuration. `--dry-run` needs neither Dagger nor the native toolchain.

## Archive smoke test

CI builds all four platform archives and runs the extracted Linux amd64 binary against synthetic consumer fixtures outside the checkout. It verifies planning, successful shared lint/HTTP checks, and a real failing HTTP cleanup diagnostic. This exercises the packaged SDK adapter and check sources, without a source-launcher fallback. Other platform binaries are cross-compiled; they are not all runtime-tested in this job.

Run the same test locally with an archive matching your host architecture:

```sh
./scripts/test-release dist/levenshtein_VERSION_darwin_arm64.tar.gz
```

The smoke test requires a Docker-compatible runtime. Snapshot builds and CI smoke tests do not publish a release; only a `v*` tag does.
