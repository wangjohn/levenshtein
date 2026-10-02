# Release archives

Releases provide CLI archives for macOS and Linux on amd64 and arm64, with SHA-256 checksums. The prepared 0.3.0 distribution is not yet published. Its archives include the Apache 2.0 license, John Wang's notice, the [changelog](../CHANGELOG.md), [contribution guide](../CONTRIBUTING.md), [security policy](../SECURITY.md), [code of conduct](../CODE_OF_CONDUCT.md), [agent conventions](../AGENTS.md), documentation, and [consumer templates](../templates), as well as the shared Dagger module, lint/tool modules, patched SDK adapter with its `uv.lock`, fixtures, and the source files used to identify its implementation. Keep the archive together so the binary and checks have the same revision.

Published v0.2.0 and earlier releases retain their MIT license and original
archive contents; the new metadata is not retroactively added. Use the license
and files shipped with your pinned release.

The release workflow prepares a draft that also holds an SPDX SBOM per archive, `checksums.txt` covering every published file, and a build provenance attestation for the archives and the checksum file, which `gh attestation verify <file> --repo wangjohn/levenshtein` checks.

## Running an archive

After extracting an archive, run the binary with explicit paths:

```sh
/path/to/archive/levenshtein pre-merge --shared /path/to/archive --source /path/to/app
```

The binary itself needs no host Go compiler. Checks bound to a Dagger environment, which includes every check of a repository with no `levenshtein.json`, need a Docker-compatible runtime; the SDK downloads the pinned Dagger CLI when needed. Checks bound to a [native environment](configuration.md#native-go-checks) need no container runtime but use the host's tools: the native Go checks need the Go version in the archive's `.go-version` on `PATH`, and native `command` checks need whatever their configuration declares. `--dry-run` needs neither.

## Pinning a release

Pin the release's commit SHA with its tag as a comment, as the examples do:
`uses: wangjohn/levenshtein@<sha> # vX.Y.Z`, where `<sha>` is
`git rev-list -n1 vX.Y.Z`. A SHA cannot be moved; the comment keeps the pin
readable in a diff and names the release notes. Pinning the tag itself
(`@vX.Y.Z`) also works but trusts the tag not to move. Either is a deliberate,
reviewed update, and Dependabot's `github-actions` ecosystem proposes the next
release, SHA and comment included. Do not pin a branch. See
[GitHub Actions](consumer-ci.md#github-actions) and [versioning](versioning.md).

## Publishing a release

Maintainers cut releases as described in [cutting a release](maintainers/releases.md#publishing-a-release).

## Protecting release tags

Moved to [cutting a release](maintainers/releases.md#protecting-release-tags).

## Archive smoke test

Moved to [cutting a release](maintainers/releases.md#archive-smoke-test).

The archive is a CLI and shared-check distribution. For contributor scripts,
repository workflows, hooks, and example rule modules, use the
[repository checkout](https://github.com/wangjohn/levenshtein). Generated local
SDK output, build output, private files, and review artifacts are not release
metadata.

Maintainers can check extracted metadata and documentation links without a
container runtime from a checkout with the pinned Go toolchain:

```sh
./scripts/test-release --metadata-only /path/to/archive.tar.gz
```

The full smoke command also runs the packaged binary and Dagger checks. A
metadata-only pass does not establish platform or consumer execution coverage.
