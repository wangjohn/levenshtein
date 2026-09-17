# Release archives

GoReleaser builds the CLI for macOS and Linux on amd64 and arm64, and creates archives with SHA-256 checksums. Each archive includes the shared Dagger module and the source files used to identify its implementation. Keep the archive together so the binary and checks have the same revision.

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
