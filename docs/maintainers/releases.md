# Cutting a release

This page is for Levenshtein's maintainers. To use a release, see [release archives](../releases.md).

GoReleaser builds the CLI for macOS and Linux on amd64 and arm64, and creates archives with SHA-256 checksums. Using Go 1.27.1 and GoReleaser 2.18.1, validate and build locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Snapshot builds create files in `dist/` and publish nothing.

## Publishing a release

An annotated tag prepares a draft GitHub release, which the maintainer reviews
and publishes after checking its assets and notes:

1. In one pull request, rename `## [Unreleased]` in `CHANGELOG.md` to
   `## [X.Y.Z] - YYYY-MM-DD` above a fresh empty `[Unreleased]`. Leave the
   consumer examples on the previous release: they pin its commit SHA, and the
   new release has none yet.
2. Merge it, then push an annotated tag on the merge commit.
   `.github/workflows/release.yml` builds and uploads the draft:

```sh
git tag -a vX.Y.Z -m 'Levenshtein vX.Y.Z'
git tag -a runner/lint/vX.Y.Z -m 'Levenshtein vX.Y.Z linter module'
git push origin vX.Y.Z runner/lint/vX.Y.Z
```

The second tag versions the nested `runner/lint` module, so
`go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@vX.Y.Z`
resolves to the same revision as the release
([running the linter directly](../rules.md#running-the-linter-directly)). It
does not match the workflow's `v*` filter, so it publishes no archives.

3. Review the draft notes and download an archive to verify its checksum and
   run the documented smoke test. Confirm that SBOMs and attestations are
   present, then publish the GitHub release. In a second pull request, move
   every consumer example to the new release:
   `wangjohn/levenshtein@<sha> # vX.Y.Z`, where `<sha>` is
   `git rev-list -n1 vX.Y.Z`, and `--branch vX.Y.Z`. Run
   `scripts/test-doc-pins --latest` before opening it: it fails until every
   example names the newest release.

On every pull request, `scripts/test-doc-pins` checks that each action pin is a
full commit SHA with its tag as a comment, that it names a tagged release in
`CHANGELOG.md`, and that the SHA is the commit the tag names. It does not
require the newest release, so pushing a tag never fails open pull requests;
only `--latest`, in step 3, does.

The workflow first runs `scripts/release-on-main`, which refuses a tag that is
not `vX.Y.Z`, whose version is not a release in `CHANGELOG.md`, or whose commit
`main` does not contain. This proves ancestry and a changelog entry; it does
not prove that the commit passed checks or received PR review. It then builds with the pinned Go from `.go-version`, installs the
pinned syft, and runs `goreleaser release --clean`.
The draft GitHub release then holds:

- the four platform archives (`linux`/`darwin` × `amd64`/`arm64`), each with the
  CLI and the shared check sources;
- an SPDX SBOM per archive;
- `checksums.txt` covering every published file;
- a build provenance attestation for the archives and the checksum file, which
  `gh attestation verify <file> --repo wangjohn/levenshtein` checks.

Publication is manual after the draft assets and notes are reviewed. The tag
is created by a person. The workflow requires its commit to be on `main`; the
maintainer must separately verify the checks and review evidence described in
[CI guarantees](ci.md#required-checks-branch-protection). `release-smoke` in the self-checks
workflow validates `.goreleaser.yaml` and builds the same archives as a snapshot
on every ready pull request, so a tag is not the first time the configuration
runs.

## Protecting release tags

The two files in `.github/rulesets/proposed/` are pending admin actions, not
live enforcement. Both cover `refs/tags/v*` and `refs/tags/runner/lint/v*`:

- `tags-creation.json` restricts creation to the maintain and admin repository
  roles through explicit bypass actors. This grants tag creation, not PR review.
- `tags.json` blocks updates, force pushes, and deletion with no bypass actors,
  including for admins. Check the target commit before pushing either tag.

The nested lint tag does not trigger the archive workflow, so it has no
workflow ancestry guard. Before tagging, run `scripts/release-on-main vX.Y.Z
<commit> origin/main`, check the release commit's checks and review evidence,
and confirm both annotated tags resolve to that exact commit. Tag protection
does not require the namespaces to point to the same commit.

Read-only inspection on 2026-10-02 found only the active main ruleset
(ID `23844934`), and no repository tag rulesets. The existing `v0.2.0` and
`v0.1.0` releases were published with `immutable: false`. These observations
are historical evidence, not a current enforcement guarantee; repeat the
inspection before launch. Required admin work remains pending:

1. Review and authorize the two tag proposals, then create the rulesets in
   **Settings → Rules → Rulesets** or with the REST API:

   ```sh
   gh api --method POST repos/wangjohn/levenshtein/rulesets --input .github/rulesets/proposed/tags.json
   gh api --method POST repos/wangjohn/levenshtein/rulesets --input .github/rulesets/proposed/tags-creation.json
   ```

   Read back each created ruleset. Record repository, inspection time, ruleset
   IDs, active enforcement, both include patterns, empty excludes, rule types,
   and bypass actors. Only after verifying the live state, export each response
   as `{name, target, enforcement, conditions, bypass_actors, rules}` into
   `.github/rulesets/` and remove the matching proposal in a pull request.
   Run `scripts/test-rulesets --live` with admin visibility. Local proposal
   validation cannot satisfy this gate, and a read-only CI token may omit
   bypass actors. Do not move a proposal merely to make CI pass.
2. Inspect **Settings → General → Releases** and enable release immutability
   after separate authorization. Read its state with:

   ```sh
   gh api repos/wangjohn/levenshtein/immutable-releases
   gh api repos/wangjohn/levenshtein/releases/tags/vX.Y.Z --jq '{tag_name, target_commitish, draft, immutable}'
   ```

   Record the repository setting response (`enabled: true`) or the settings
   UI with time and repository identity. It applies to releases published after
   it is enabled; it does not retroactively make old releases immutable.
   Before publishing, verify the draft's assets, checksums, SBOMs, provenance,
   notes, and both tag commit SHAs. After separately authorized publication,
   record `draft: false` and `immutable: true` for the release. A pushed tag or
   successful draft workflow is not proof of publication or immutability.
3. Inspect **Settings → Code security → Private vulnerability reporting**:

   ```sh
   gh api repos/wangjohn/levenshtein/private-vulnerability-reporting
   ```

   Record `enabled: true` with time and repository identity, plus the public
   **Security → Advisories → Report a vulnerability** entry point. Enable the
   setting only after authorization if absent. A permission error is unknown,
   not evidence that reporting is enabled or disabled. No vulnerability report
   needs to be submitted to inspect the setting.
4. Recheck effective main protections and the dependency graph as described in
   [CI guarantees](ci.md#required-checks-branch-protection), including inherited
   rules and bypass policy. Save sanitized API responses or UI evidence and
   the final candidate SHA in the launch validation record. Unknown or pending
   settings block the launch settings gate, even when this code PR is ready.

## Archive smoke test

CI builds all four platform archives and runs the extracted Linux amd64 binary against synthetic consumer fixtures outside the checkout. It verifies planning, successful shared lint/HTTP checks, and a real failing HTTP cleanup diagnostic. This exercises the packaged SDK adapter and check sources, without a source-launcher fallback. Other platform binaries are cross-compiled; they are not all runtime-tested in this job.

Run the same test locally with an archive matching your host architecture:

```sh
./scripts/test-release dist/levenshtein_VERSION_darwin_arm64.tar.gz
```

The smoke test requires a Docker-compatible runtime. Snapshot builds and CI smoke tests do not publish a release; only a `v*` tag does.
