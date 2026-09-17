# Release archives

GoReleaser builds the CLI for macOS and Linux on amd64 and arm64, and creates archives with SHA-256 checksums. Each archive includes the shared Dagger module, lint/tool modules, patched SDK adapter, fixtures, and the source files used to identify its implementation. Keep the archive together so the binary and checks have the same revision.

Using Go 1.27.1 and GoReleaser 2.18.1, validate and build locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Snapshot builds create files in `dist/` and publish nothing. Release publication and tag automation are not configured yet.

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

The smoke test requires a Docker-compatible runtime. Snapshot builds and CI smoke tests do not publish a release. Until publication is configured, consumers can pin a reviewed source checkout or use an archive they build from that revision.
