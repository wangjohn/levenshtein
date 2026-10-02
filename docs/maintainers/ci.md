# Levenshtein's own CI

This page is for Levenshtein's maintainers. The checked-in `Levenshtein self-checks` workflow, `.github/workflows/verify.yml`, verifies this repository's runner and fixtures; its cron schedules that verification only. Application repositories call the shared runner from their own CI, as shown in [using it in CI](../consumer-ci.md). To build and test locally, see [developing Levenshtein](development.md).

## Jobs

| Job | Role |
| --- | --- |
| `lint` | Static `./verify branch` only, on the native executor (early signal; no host race tests or consumer regressions) |
| `tests` | Host race/fixtures, `shellcheck`, SDK restore or regen, non-lint Dagger checks, consumer regressions |
| `language-contracts` | Rust and Python contract fixtures |
| `action` | The root `action.yml` as a consumer calls it, on a native fixture: one passing run and one that must fail with the planted finding |
| `macos` | The root module's unit tests on `macos-latest`, without Dagger; not a required check |
| `release-build` | Validate GoReleaser, build all four snapshot archives and SBOMs, and run the extracted Linux amd64 Dagger consumer smoke (skipped on draft PRs) |
| `release-platform` | Execute those same archives on Linux amd64/arm64 and macOS amd64/arm64 matching hosted runners |
| `release-smoke` | Stable required aggregate: succeeds only when the build and all four runtime checks succeed (skipped on draft PRs) |
| `semantic-lint` | Advisory Jev review of the pull request; runs only on `pull_request` events; without the `TYPESAFE_API_KEY` secret the review step is skipped and the job passes with no findings |
| `mutation` | [Mutation testing](../mutation.md) of the Go files the pull request changed in the root module and `runner/lint`, as `./verify mutation`; runs only on `pull_request` events, with the full history so the merge base exists. `runner` is left out: its tests need a live Dagger session, which gremlins cannot give each mutant |

The `tests` job also holds the repository hygiene gates, all of them before its
Go tests: `gofmt` over every tracked Go file outside `testdata`, whose lint
fixtures are deliberately unformatted; `ruff check` over `scripts` and
`sdk/patched-go`; `scripts/test-sdk-lock`, which requires `sdk/patched-go/uv.lock`
to be current, hashed, and on the `dagger-io` of `.dagger-version`;
`scripts/test-doc-pins`, after fetching the tags, which requires every consumer
example to pin a tagged release in `CHANGELOG.md` by that tag's commit SHA; and
`scripts/test-workflows`, which requires one pin per action across the
workflows, templates and docs (Dependabot updates the workflows and the
template in one grouped pull request; `scripts/sync-action-pins` copies its
pins into the docs), per-commit concurrency groups outside pull
requests, a `merge_group` trigger on every workflow with a required check, and
secrets only in step `env`. The Go test step writes a coverage profile that is uploaded
as an artifact for seven days; no threshold gates the run. `scripts/test-integration`
then runs every `integration`-tagged test: natively before the Dagger setup,
and the few it lists as needing Dagger after it, so a new integration test runs
in CI without a workflow change.

Module manifests are not a step of their own: the `lint` job's `branch` run
includes the shared [`go-mod` check](../check-kinds-guide.md#module-manifests)
(`go mod tidy -diff` and `go mod verify`) over `.`, `runner/lint`,
`runner/community`, each tool module under `runner/tools`, and
`examples/rule-module` on every event, and never reuses a cached result. `runner` is
left out because `dagger develop` rewrites its manifest.

The Go tests stay steps of the `tests` job, which runs `go test -race` over the
root module (with a coverage profile), `runner/lint`, `runner/community`, and,
through `scripts/test-example-rules`, `examples/rule-module`. `levenshtein.json` has
a native [`go-test`](../check-kinds-guide.md#tests) run over the same modules for local
use, but no CI run includes it, because it would run those tests a second time.

The same `branch` run includes the shared
[`workflow-security` check](../check-kinds-guide.md#workflow-security): zizmor's offline audits
of the workflows, composite actions, and Dependabot configuration, failing on
findings of medium severity and above, natively in `branch` and `pre-merge` and
in Dagger in `branch-dagger` and `main`. So do [`shell-lint`](../check-kinds-guide.md#shell-scripts)
over `scripts/` and `verify`, and [`secrets`](../check-kinds-guide.md#secrets) over the whole
repository less `runner/testdata/secrets-leaky`, the fixture that exists to be
found. Its only dependencies outside Go are the patched SDK adapter's locked
Python packages, which a [`deps-vuln`](../check-kinds-guide.md#dependency-vulnerabilities)
check scans in `main` and in `vulnerabilities.yml`.

`security.yml` runs beside it: zizmor's GitHub Action with the workflow token on
every pull request, push to `main`, and weekly, so the audits that query GitHub
(`impostor-commit`, `known-vulnerable-actions`, `ref-confusion`), which the
offline shared check cannot run, still gate changes and notice new advisories.
It names the same inputs as the shared check plus both consumer workflow
templates, so the deliberately insecure fixture under `runner/testdata` stays
out of it, and runs the zizmor version
`runner/toolchain.json` pins (a test keeps the two equal); OpenSSF Scorecard with a SARIF upload to code
scanning on `main` and the weekly schedule, since Scorecard reads the default
branch rather than a pull request's merge ref; and `dependency-review` on pull
requests, failing on high severity. `dependency-review` needs the repository's
**Dependency graph**, which is a repository setting (Settings → Code security)
and not something a workflow can enable. Inspect **Settings → Code security** and record its enabled state before
launch; the job fails if it is off, rather than passing without a review.

`release.yml` publishes the archives, their SBOMs, `checksums.txt`, and a build
provenance attestation when a `vX.Y.Z` tag is pushed; see
[releases](releases.md).

The platform jobs use `ubuntu-24.04`, `ubuntu-24.04-arm`, `macos-15-intel`,
and `macos-15`, respectively, from GitHub's
[standard public runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
Each downloads the current run's snapshot artifact, verifies its commit and
`--version` platform identity, checks extracted metadata and documentation,
plans a native run, passes a clean fixture, and requires exit 1 plus `SA5001`
from a deliberately broken fixture. They use the pinned Go toolchain without
Docker or restored module caches. Cross-compilation alone is not a runtime
check. The Linux amd64 Dagger smoke remains in `release-build`.

To reproduce one native platform check locally, build a snapshot, then run:

```sh
./scripts/test-release --native PATH_TO_ARCHIVE VERSION FULL_COMMIT darwin amd64
```

Use the version and commit in `dist/metadata.json` and your host's OS and
architecture. Archive discovery supports nested module-tag snapshot names;
this affects only smoke tests, and does not change real release tag validation.

Event → `./verify` mapping. `branch` and `pre-merge` run the static Go checks on the native executor; `main` and `self-test` run in Dagger. A push to `main` therefore runs no Dagger lint, and the scheduled `main` audit is the daily hermetic pass over every Go check.

- Draft PR / push to `main`: `lint` runs `branch`; `tests` skips Dagger verify (lint already covered static checks).
- Ready PR / merge queue: `lint` runs `branch`; `tests` runs `self-test` (together equivalent to `pre-merge`).
- Daily schedule (07:23 UTC): `lint` runs `branch`; `tests` runs `main` (fresh audit + `go-vuln`).
- Manual dispatch: `lint` runs `branch`; `tests` runs the requested run (default `self-test`, since `lint` already covers the static checks in `pre-merge`).
- Any PR, draft or ready: `semantic-lint` runs `semantic-lint` and `mutation` runs `mutation`, each in its own run outside `pre-merge`.

## Required checks (branch protection)

| When | Require |
| --- | --- |
| Early PR progress (including drafts) | **`lint`** |
| Merge / ready-for-review / merge queue / `main` | **`lint`**, **`tests`**, **`language-contracts`**, **`action`**, **`release-smoke`** |
| Pull requests | **`dependency-review`** (fails on a high-severity dependency added by the pull request, or if the dependency graph is off) |
| Pull requests | `semantic-lint` (advisory; not required to pass; the job passes with no findings when `TYPESAFE_API_KEY` is absent) |
| Pull requests | `mutation` (not required to pass, like `semantic-lint`; `scripts/test-rulesets` keeps the two required together or not at all; it fails on a surviving mutant on a changed line, and exceptions go in `.levenshtein/mutation-accepted.json` or `runner/lint/.levenshtein/mutation-accepted.json`) |

The table describes workflow execution and merge expectations, not conditional
GitHub requirements. The committed main ruleset requires all six listed status
contexts, including on drafts; draft jobs can skip expensive steps and still
report success. A draft check result is therefore not full release-smoke or
container validation. Rerun and inspect the ready PR jobs before merging.

Read-only inspection on 2026-10-02 found the active main ruleset `23844934`
matching `.github/rulesets/main.json`: deletion and non-fast-forward protection,
six GitHub Actions status contexts (integration ID `15368`),
`strict_required_status_checks_policy: false`, and admin-role bypass mode
`always`. It contains no `pull_request` rule. Consequently:

- Main ancestry proves that the branch contains a commit; it proves neither
  successful checks nor review of that commit.
- Required checks constrain actors subject to the ruleset. The current setting
  does not require a branch to be up to date with main before merging.
- The ruleset does not require a PR or a human approval. Maintainer review is a
  release procedure, not an enforced guarantee. A required-PR rule and any
  approval count need a separate maintainer decision; do not require a second
  human reviewer by accident.
- Admins can bypass this main ruleset. A successful merge alone does not prove
  the checks or review ran. Record the exact candidate SHA and successful ready
  checks, and the independent review evidence used for the release decision.

Requiring up-to-date checks is a pending separately authorized admin action;
keep `main.json` as the observed state until a verified live change is exported.
To inspect effective protections, record the repository, time, default branch,
and candidate SHA, then run:

```sh
gh api 'repos/wangjohn/levenshtein/rulesets?includes_parents=true'
gh api repos/wangjohn/levenshtein/rulesets/23844934
gh api repos/wangjohn/levenshtein/rules/branches/main
gh api repos/wangjohn/levenshtein/branches/main/protection
```

Inspect inherited rules, active/evaluate/disabled enforcement, ref include and
exclude patterns, required contexts and integration IDs, strictness, PR rules
and approval counts, and all bypass actors. Repository snapshot comparison
alone does not inspect inherited rules or classic branch protection. The
connector's classic branch-protection request returned 403 during the above
inspection; that state remains unknown until an authorized API or settings UI
inspection succeeds. Save sanitized responses or UI evidence; missing access
cannot establish a protection guarantee.

Keep draft progress fast by skipping expensive steps, while retaining the
stable `release-smoke` and `tests` status contexts. When adopting this workflow, replace any required check named `verify` with `lint` and `tests` the same day.

GitHub enforces these from a repository ruleset, which lives in repository settings rather than in a file: rules a pull request could edit would let that pull request weaken them. `.github/rulesets/main.json` is the reviewed copy, and `scripts/test-rulesets --live` in the `tests` job keeps it honest:

- every required check must name a job in `.github/workflows` (its `name:` if it has one, otherwise its ID), so renaming or relabeling a required job fails the pull request instead of leaving later merges waiting on a check that never reports;
- the rulesets GitHub enforces must match the committed copies. The run's read-only token cannot see `bypass_actors`, so CI compares them only when an admin runs the script locally.

To change a rule, edit it in **Settings → Rules → Rulesets**, then export the new state in the same pull request that needs it:

```sh
gh api repos/wangjohn/levenshtein/rulesets/<id> \
  | jq '{name, target, enforcement, conditions, bypass_actors, rules}' > .github/rulesets/main.json
```

To rename a required job, coordinate the proposed workflow and required context changes in one pull request. Its drift check fails until an admin changes the live ruleset to match; do that just before merging, then rerun `tests`.

Treat warm lint wall time creeping toward warm tests as a CI performance regression. Read step and job durations from the Actions run view and compare medians across warm `ubuntu-24.04` runs.

## Result-cache trust

- Restore `verification-v1-lint` / `verification-v1-tests` on every event, and save on every event too. A pull request run saves into its own merge-ref scope, which only reruns of that PR can restore and `main` never reads, so a fork run cannot seed `main`'s entries.
- Lint and tests use **separate** verification keys so they cannot race one entry. The generated SDK still uses an exact key with no `restore-keys`.
- `merge_group` is not one of the events that writes the default-branch cache scope, so its saves are invisible to `main`; the tests scope on `main` is seeded by schedule and `workflow_dispatch` runs. The other direction is open: a pull request, including one from a fork, can restore `main`'s entries. They hold verification results and generated code, never secrets.
- Entries are small JSON records, and the SDK entry is touched on every run, so result entries do not push it out of the repository's 10 GB Actions cache budget. The native checks also keep `staticcheck/` and `tools/` inside the verification cache directory, so the restore and save steps name only the record directories, `results/` and `stat/`. An `actions/cache` exclusion such as `!dir/tools` does not apply beneath a directory the path list already includes, so excluding them that way would still save them.
- The `lint` job keeps a separate Staticcheck cache, `staticcheck-v1-<os>-<hash of .go-version, runner/lint/**, runner/toolchain.json>-<sha>`, restored by prefix. Only a push to `main` and the schedule save it; pull requests, including forks, restore `main`'s entry and never write one. Staticcheck salts every entry with a hash of the linter binary, so an entry built by a different linter only misses; keying on everything that builds the linter starts a fresh cache when it changes instead of carrying dead entries until Staticcheck trims them after five days unused. A fresh run (`rerun_checks`) uses a throwaway directory, so a restored cache can only save work. The job summary lists each check's status, cache status and duration, and the size of both cache entries.
- Scheduled `main` keeps `rerun_checks: true`; `go-vuln` always re-executes.

## Caches and self-config notes

The `lint` and `tests` jobs (and `vulnerabilities`) restore the pinned Dagger release archive from the Actions cache when `.dagger-version` / `scripts/dagger-checksums.txt` are unchanged, and download it on a miss. The cache holds the archive, never the binary: `scripts/install-dagger` checks it against `scripts/dagger-checksums.txt` on every run, replaces one that does not match, and installs the CLI from it. That block and the generated-SDK restore/generate/save block each live in one composite action (`.github/actions/setup-dagger`, `.github/actions/dagger-sdk`) rather than being repeated per job. `language-contracts` uses the setup-go module cache over root `go.sum`.

Generated SDK (`runner/dagger.gen.go`, `runner/internal/dagger`, `runner/internal/telemetry`) uses **exact** key `dagger-sdk-v2-…` (no `restore-keys`). Restore + save are separate steps; save runs only when `scripts/ci-dagger-sdk` reports `ready=true` after a miss. Readiness is decided by compiling: a restored SDK is usable exactly when `go build ./...` succeeds in `runner`, which needs no Dagger session. Anything that fails to compile — including a truncated or poisoned entry — is regenerated, and the script `mkdir`s `internal/telemetry` so the cache save still finds every path when codegen emits no sources there. Bump the `vN` prefix to abandon a stuck key. Warm check: Generate reports a reused SDK instead of running `dagger develop`. Invalidate when any of these change: `dagger.json`, `.dagger-version`, `scripts/dagger-checksums.txt`, `runner/go.mod`, `runner/go.sum`, `runner/toolchain.json`, `runner/*.go`, `sdk/patched-go/**`. `vulnerabilities.yml` still always runs `scripts/test-sdk-security`.

Completed verification results are restored into `$RUNNER_TEMP/levenshtein-verification-v1` and passed to `./verify --cache-dir` on `lint` / `tests` with per-job keys (`verification-v1-lint-${{ runner.os }}-<sha>`, `verification-v1-tests-${{ runner.os }}-<sha>`). The key is deliberately per-commit with a prefix `restore-keys` fallback: the CLI already fingerprints each target, so handing it the newest earlier directory lets it reuse the entries that are still valid instead of missing the whole cache on any source edit. Save runs on every event and on failed runs, since results are keyed by content and a pull request's cache is scoped by GitHub to that PR and its base branch. Warm check: a rerun of the same commit hits the exact key and skips Save; a new commit reports a `restore-keys` match.

In-engine Go module/build and Staticcheck `CacheVolume`s remain version-keyed in `runner/` but are **session-local** on ephemeral GitHub-hosted runners. Persisting those volumes across VMs is **blocked** for Dagger **0.21.9** (no supported CI export/restore API without experimental hacks). This is why `branch` and `pre-merge` run natively: the host's Go build cache (through setup-go) and the Staticcheck cache above do persist.

Self-config targets use narrow literal `inputs` (not `"."`): root Go module paths, `runner` / `runner/lint`, `.github/workflows` for workflow-lint, `.github` plus `action.yml` for workflow-security, and `scripts` plus `verify` for shell-lint; `secrets` keeps `"."` less its leaky fixture, since a secret can be committed anywhere. The `runner` module compiles against the gitignored generated SDK, which git discovery does not list. Its key still changes when the SDK does, because the Go kinds also cover the ignored Go files their toolchain loads; `TestSelfVerificationFingerprintsTheGeneratedSDK` pins that. Doc-only edits therefore do not invalidate Go analysis result fingerprints. The one exception is the `repository` target, which keeps `"."` so `semantic-lint` still judges Markdown and workflow changes and so the `go-test` run over the root module, whose tests read `runner/testdata`, is keyed on every file they can read. Independent checks in a run execute concurrently (bounded workers) inside `./verify`.

## Success criteria and gaps

| Criterion | Status |
| --- | --- |
| Lint ≪ tests (warm lint well under 1 min) | In progress — `branch` now runs natively with a persisted Staticcheck cache; measure warm runs on `main` |
| Coverage preserved on ready/merge/`main`/schedule | Met by job split + event mapping |
| Result-cache isolation between untrusted PRs and `main` | Met (PR-written caches stay in the PR's merge-ref scope, which `main` never reads) |
| Freshness (`rerun_checks` / `go-vuln`) | Met |
| Self-CI scope (not consumer packaging) | Met |

## Out of scope here

Consumer CLI `--source` cache UX, install/release packaging, and Swift macOS lint jobs for native application repos are tracked in other workstreams (consumer CLI performance, setup packaging, Go–Swift lints), not in this self-CI workflow.

Pre-split baseline (monolithic `verify` ~4.6–5 min on `ubuntu-24.04`; `dagger develop` ~93s; `./verify` ~94–114s): Actions run [35283983398](https://github.com/wangjohn/levenshtein/actions/runs/35283983398).

