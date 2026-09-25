# Release archives

GoReleaser builds the CLI for macOS and Linux on amd64 and arm64, and creates archives with SHA-256 checksums. Each archive includes the shared Dagger module, lint/tool modules, patched SDK adapter with its `uv.lock`, fixtures, and the source files used to identify its implementation. Keep the archive together so the binary and checks have the same revision.

Using Go 1.27.1 and GoReleaser 2.18.1, validate and build locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Snapshot builds create files in `dist/` and publish nothing.

## Publishing a release

Publication is a tag, prepared by a release pull request:

1. In one pull request, rename `## [Unreleased]` in `CHANGELOG.md` to
   `## [X.Y.Z] - YYYY-MM-DD` above a fresh empty `[Unreleased]`. Leave the
   consumer examples on the previous release: they pin its commit SHA, and the
   new release has none yet.
2. Merge it, then push an annotated tag on the merge commit.
   `.github/workflows/release.yml` does the rest:

```sh
git tag -a vX.Y.Z -m 'Levenshtein vX.Y.Z'
git push origin vX.Y.Z
```

3. In a second pull request, move every consumer example to the new release:
   `wangjohn/levenshtein@<sha> # vX.Y.Z`, where `<sha>` is
   `git rev-list -n1 vX.Y.Z`, and `--branch vX.Y.Z`.

`scripts/test-doc-pins` holds the examples to that: every action pin is a full
commit SHA with its tag as a comment, the SHA is the commit the tag names, and
the version is the newest release in `CHANGELOG.md`, or the one before it only
until the newest is tagged. Once the tag exists, every pull request fails the
check until step 3 merges, so tag promptly and follow with it.

The workflow first runs `scripts/release-on-main`, which refuses a tag that is
not `vX.Y.Z`, whose version is not a release in `CHANGELOG.md`, or whose commit
`main` does not contain, so a tag pushed on an unreviewed commit publishes
nothing. It then builds with the pinned Go from `.go-version`, installs the
pinned syft, and runs `goreleaser release --clean`.
The GitHub release then holds:

- the four platform archives (`linux`/`darwin` × `amd64`/`arm64`), each with the
  CLI and the shared check sources;
- an SPDX SBOM per archive;
- `checksums.txt` covering every published file;
- a build provenance attestation for the archives and the checksum file, which
  `gh attestation verify <file> --repo wangjohn/levenshtein` checks.

Nothing else is automated: the tag is created by a person, and a release is only
as reviewed as the commit it points at, which the workflow requires to be on
`main`. `release-smoke` in the self-checks
workflow validates `.goreleaser.yaml` and builds the same archives as a snapshot
on every ready pull request, so a tag is not the first time the configuration
runs.

## Pinning a release

Pin the release's commit SHA with its tag as a comment, as the examples do:
`uses: wangjohn/levenshtein@<sha> # vX.Y.Z`, where `<sha>` is
`git rev-list -n1 vX.Y.Z`. A SHA cannot be moved; the comment keeps the pin
readable in a diff and names the release notes. Pinning the tag itself
(`@vX.Y.Z`) also works but trusts the tag not to move. Either is a deliberate,
reviewed update, and Dependabot's `github-actions` ecosystem proposes the next
release, SHA and comment included. Do not pin a branch. See
[GitHub Actions](consumer-ci.md#github-actions).

## Protecting release tags

A release is only as trustworthy as its tag. The release workflow already
refuses a tag that `main` does not contain; two repository settings, which a
workflow cannot change, stop a tag from being moved or deleted once pushed.
An admin applies them:

1. Create the tag rulesets in `.github/rulesets/proposed`: `tags.json` blocks
   updating, force-pushing and deleting any `v*` tag, for everyone, and
   `tags-creation.json` lets only the admin and maintain roles create one.

   ```sh
   gh api --method POST repos/wangjohn/levenshtein/rulesets --input .github/rulesets/proposed/tags.json
   gh api --method POST repos/wangjohn/levenshtein/rulesets --input .github/rulesets/proposed/tags-creation.json
   ```

   Then, in a pull request, move both files up to `.github/rulesets/` so
   `scripts/test-rulesets --live` compares them with what GitHub enforces.
   Until then that script only checks that the proposals are well formed.
2. Turn on immutable releases, so a published release's tag and assets cannot
   change either:

   ```sh
   gh api --method PUT repos/wangjohn/levenshtein/immutable-releases
   ```

   `gh api repos/wangjohn/levenshtein/immutable-releases` reports
   `"enabled": true` once it is on. It applies to releases published after it
   is enabled.

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
