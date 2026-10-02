# Cutting a release

This page is for Levenshtein's maintainers. To use a release, see [release archives](../releases.md).

GoReleaser builds the CLI for macOS and Linux on amd64 and arm64, and creates archives with SHA-256 checksums. Using Go 1.27.1 and GoReleaser 2.18.1, validate and build locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Snapshot builds create files in `dist/` and publish nothing.

## Release evidence record

Prepare 0.3.0 as a minor release: newly detected failures and optional interfaces
make it unsuitable for a 0.2.x patch. The changelog's undated 0.3.0 section is
preparation, not permission to tag or publish. Complete the pre-tag evidence
against the final integrated candidate before finalizing its release date.
After that pull request merges, verify the finalized main commit's date,
checks and reviews before tagging. Record tag and draft evidence after draft
preparation, then publication and immutability evidence after publication;
those later observations cannot be prerequisites for creating the tags.
Record evidence links, full commit SHAs and inspection times; use **pending** or
**unknown** when evidence is unavailable. Source implementation, tagging, live
settings changes and publication are separate actions.

| Gate | Evidence to record | Preparation status |
| --- | --- | --- |
| Candidate and review | Final main/candidate full SHA, ancestry, changelog version/date, independent reviews and repaired findings; both root and nested lint tags must resolve to it | Pending final integration and review; an intermediate stack is not a launch candidate |
| Required CI and runtime | Ready-PR required contexts at that candidate, run/job links, planned four host-matched archive results after PR #102, Linux Dagger smoke, source tests and full snapshot/metadata results | Pending final candidate runs; draft jobs and local checks do not establish hosted coverage |
| SDK security | Regeneration/runtime build and vulnerability-scan evidence for the effective SDK dependencies and packaged tools at the candidate | Pending final candidate security results; preserve failed-run dispositions |
| Live protections and security settings | Main strictness, effective/inherited rules and bypasses, both tag namespaces, dependency graph, immutable releases and private reporting; sanitized responses/UI evidence | Repeat inspection and resolve pending authorized settings changes; proposals are not enforcement |
| Secret provenance | Maintainer resolution of historical scan provenance and any required remediation, with only redacted disposition evidence | Pending maintainer resolution; do not retrieve, reproduce or authenticate suspected credentials |
| Performance/adoption statements | Repaired measurement methodology, complete samples and validated cache assumptions if numbers are published; real pilot observations if adoption claims are made | Pending integration and validation of measurement methodology; public claims need final-candidate evidence |
| Draft and publication | Archives/checksums/SBOMs/attestations, smoke results, notes and both exact tag SHAs; separate publication authorization and observed `draft: false`/immutability afterwards | Pending authorized tags, draft review and publication; update consumer pins only afterwards |

Complete integration of the planned follow-up PRs before claiming their
coverage: PR #99 prepares contributor tiers, PR #102 prepares four-platform
archive execution, PR #103 prepares performance measurement methodology,
PR #110 repairs native executable publication, and PR #111 documents README
product boundaries. Until each change is merged and checked at the final
candidate, treat its coverage and claims as pending. This evidence
record itself does not introduce those features or certify their results.

Inspect live protections and security settings again at launch, including
inherited rules and bypasses. Proposals are not enforcement, and unavailable
settings are unknown. See [CI guarantees](ci.md#required-checks-branch-protection)
and [protecting release tags](#protecting-release-tags) for required read-back.
Keep secret disposition evidence redacted. A lack of real adoption pilots is
an evidence limit, not grounds for invented testimonials.

## Publishing a release

An annotated tag prepares a draft GitHub release, which the maintainer reviews
and publishes after checking its assets and notes:

1. After the pre-tag evidence is complete, finalize the prepared version in a
   pull request: replace its undated heading with `## [X.Y.Z] - YYYY-MM-DD`
   below the fresh empty `[Unreleased]`. For an ordinary release without a
   prepared section, move the Unreleased entries into that dated heading. Leave the
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
   present, then follow the [Marketplace publication checklist](#github-marketplace)
   before publishing the GitHub release. In a second pull request, move
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
not `vX.Y.Z`, whose candidate commit lacks a dated release heading in
`CHANGELOG.md`, or whose commit `main` does not contain. This proves ancestry and a changelog entry; it does
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

## GitHub Marketplace

Publish the root action alongside the reviewed CLI release so consumers can
find it in Marketplace and copy its installation syntax. GitHub's
[publishing instructions](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/publish-in-github-marketplace)
are the source of truth for eligibility and the release form.

For the first listing:

1. Confirm the repository is public and its contents meet GitHub's action
   publishing requirements. It has one root `action.yml`; the nested helper
   actions are not separate Marketplace listings. The shared runner and check
   sources are used by the root action. Let GitHub validate the action name
   `Levenshtein verify` for uniqueness and resolve any reported metadata errors.
   The description must be fewer than 125 characters. GitHub validates metadata
   at the selected release tag, so changing `main` does not repair an older
   release. Publish a new reviewed release containing the correction; never
   move a published tag to repair Marketplace metadata.
2. Open `action.yml` on GitHub and follow its publication banner, or open the
   reviewed draft release's edit form. If an eligible published release can
   be listed through its edit form, use that release without moving its tag.
   Otherwise prepare a new release through the process above.
3. The repository owner accepts the GitHub Marketplace Developer Agreement if
   the form requires it. This is an owner action; a CLI release upload does not
   accept the agreement or publish the action to Marketplace automatically.
4. Select **Publish this Action to the GitHub Marketplace**. Resolve validation
   errors until GitHub confirms the metadata is valid. Choose the closest
   available code quality category and, optionally, security as a secondary
   category. Review the version, release notes, and validated assets, then
   publish or update the release. GitHub requires two-factor authentication.
5. Open the resulting public listing. Check the name, description, README,
   displayed version, repository link, and copied installation syntax.
6. In the consumer documentation follow-up PR, add a Marketplace badge beside
   the README's existing badges and a **Use this Action** link in its setup
   section and `docs/consumer-ci.md`. Use the actual listing URL verified in
   step 5, not a guessed slug. Update release pins through the normal process
   and run `scripts/test-doc-pins --latest` and `scripts/test-workflows`.

For every subsequent release, check the Marketplace publication option before
publishing, then verify that the public listing offers the new version. Keep
its links and the consumer workflow pins current in the documentation PR.

An Action listing supplies workflow syntax to copy into a repository. It does
not provide a GitHub App's repository-selection installation flow.

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

CI builds all four snapshot archives and SBOMs. The extracted Linux amd64
Dagger consumer smoke runs outside the checkout and verifies planning,
successful shared lint/HTTP checks, and a deliberate failing HTTP cleanup
diagnostic. It exercises the packaged SDK adapter and shared sources without a
source-launcher fallback.

PR #102 plans a separate matrix to execute each extracted archive on a matching
Linux or macOS amd64/arm64 host, checking build identity, native planning,
shared lint, and a deliberate SA5001 failure. Until that PR is merged, the
current job cross-compiles the other platforms without executing them. After
integration, record actual final-candidate matrix results; a workflow definition
alone does not establish coverage, Dagger execution on every platform, or every
check kind.

Run the packaged Dagger consumer smoke with a matching archive and a working
Docker-compatible runtime:

```sh
./scripts/test-release dist/levenshtein_VERSION_darwin_arm64.tar.gz
```

For metadata and extracted documentation links only, use
`./scripts/test-release --metadata-only /path/to/archive.tar.gz`; it needs the
pinned Go toolchain but no container. It proves no consumer execution coverage.
Snapshot builds and CI smoke tests publish nothing. A valid `vX.Y.Z` tag triggers
**draft preparation**; publication is a separate authorized maintainer action
after the assets, notes and evidence gates have been checked.
