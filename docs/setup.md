# Setup and usage

Shared Go checks run pinned correctness, error handling, enum, resource, workflow, and vulnerability tools in Dagger. The runner accepts a source checkout and a named run; your existing CI supplies workers and decides when to invoke it. See [use from an application repo](consumer-ci.md) for local and CI examples. Native commands and local result/setup/build caching are supported. See [architecture](architecture.md) for how the runner works, and [the roadmap](roadmap.md) for planned work.

## Prerequisites

The source launcher needs a `go` on `PATH`, of any version: it sets `GOTOOLCHAIN` to the version in `.go-version` (**1.27.1**), so Go downloads and caches that toolchain itself when the host differs. That download needs a reachable module proxy; with `GOPROXY=off`, install Go 1.27.1 and the launcher uses it directly. A Docker-compatible container runtime is needed only for checks bound to a **Dagger** environment; the Go SDK then downloads and checksum-verifies Dagger **0.21.9** automatically. Planning, native commands, and the [shared Go checks on a native environment](configuration.md#native-go-checks) do not start Dagger and need no container runtime; the native Go checks use the host's own `go` on `PATH`.

To develop the shared Dagger module or use `dagger check` directly, install the pinned CLI with the checked-in archive checksums:

```sh
./scripts/install-dagger
export PATH="$HOME/.local/bin:$PATH"
dagger version
```

The installer supports macOS Intel/Apple Silicon and Linux amd64/arm64. On macOS, one option for the container runtime is Colima:

```sh
brew install colima docker
colima start levenshtein --runtime docker --vm-type vz --cpu 2 --memory 4 --disk 60
```

Docker Desktop or an existing Docker engine also works. CI uses the Docker engine supplied by the GitHub-hosted Ubuntu runner. Dagger downloads its pinned engine and builds the module on the first invocation; allow extra time for the initial run.

Generate the ignored SDK files once before verifying Levenshtein itself:

```sh
dagger develop --compat=skip
```

## Run checks

From the Levenshtein checkout:

```sh
./verify                       # branch: shared Go lint and vet
./verify pre-merge             # selected lint, resource, workflow, and fixture checks
./verify main                  # fresh audit, including vulnerability scans
./verify go-lint --source /path/to/a/go/repo
./verify pre-merge --dry-run    # print selected checks without running them
./verify semantic-lint          # advisory Jev review; set TYPESAFE_API_KEY first
```

`--dry-run` plans in the standalone CLI without Dagger or its engine. A run name selects checks; it does not switch Git branches or fetch code. The passed source directory is what gets verified. CI supplies the PR/merge candidate or default-branch checkout.

Success prints a JSON report. Lint failures include native rule IDs, locations, and messages in the report; the process exits nonzero. Tool errors, invalid configuration, and modules with no Go packages fail rather than reporting an empty pass. Reports account for every selected check, including errors and cancelled or incomplete work. Durable result files and cross-run history are deferred.

Dependency resolution follows Go's defaults: use `vendor/` when enabled by the module or workspace, otherwise use read-only module resolution. Source filtering excludes `.git`, `.env`, and `.env.*` at every level, with an explicit exception for public `.env.example` templates so Go can embed them. Keep those templates free of secrets; other `.env.*` names remain excluded.

### What the rules catch

| Rule | Mistake |
| --- | --- |
| [SA5001](https://staticcheck.dev/docs/checks/#SA5001) | Deferring `Close` before checking whether opening the resource failed |
| [SA5003](https://staticcheck.dev/docs/checks/#SA5003) | Deferring work inside an infinite loop that will never execute it |
| [SA9001](https://staticcheck.dev/docs/checks/#SA9001) | Deferring cleanup inside a channel-range loop that may never finish |

These are selected Staticcheck rules, not a complete resource-leak analysis. They do not flag every defer inside an ordinary slice or counted loop, prove every resource is closed, or enforce handling `Close` errors. The valid fixtures show error checking before `defer` and cleanup scoped to a function that returns each iteration.

## Configure a repo

A single Go module at the source root works without configuration. For multiple modules or custom runs, add a version 1 `levenshtein.json` to that repo.

Use the [version 1 consumer example](consumer-ci.md#the-same-command-locally-and-in-ci) for explicit product targets, checks, and run selections. `inputs` restricts Dagger's imported source as well as its cache scope; include required manifests, local dependencies, and fixtures. Native command inputs only describe cache scope and do not restrict host access. See [source boundaries](configuration.md#source-boundaries).

A configuration file replaces defaults. Paths are relative to the source root. Every selected check runs or reuses an eligible result; change-based selection is not implemented. Available shared kinds are listed in [shared checks](checks.md#named-checks-and-suggested-runs). Every configuration file declares `"version": 1`.

Version 1 runs use explicit `rerun_checks: true` for fresh audits, regardless of their name. Levenshtein's own `main` is configured that way. Audits bypass passing-verdict reuse while retaining compatible downloads and compiler caches. Vulnerability scans always execute against current advisory data. Add new checks explicitly to your configured full run during this pilot.

## Levenshtein's own CI

The checked-in `Levenshtein self-checks` workflow verifies this repo's runner and fixtures. Its cron schedules that verification only. This section is where that CI is explained. Application repos call the shared runner from their own CI, as shown in the [consumer guide](consumer-ci.md).

### Jobs

| Job | Role |
| --- | --- |
| `lint` | Static `./verify branch` only (early signal; no host race tests or consumer regressions) |
| `tests` | Host race/fixtures, `shellcheck`, SDK restore or regen, non-lint Dagger checks, consumer regressions |
| `language-contracts` | Rust and Python contract fixtures |
| `action` | The root `action.yml` as a consumer calls it, on a native fixture: one passing run and one that must fail with the planted finding |
| `release-smoke` | `goreleaser check`, then a GoReleaser snapshot + archive test (skipped on draft PRs) |
| `semantic-lint` | Advisory Jev review of the pull request; runs only on `pull_request` events; without the `TYPESAFE_API_KEY` secret the review step is skipped and the job passes with no findings |

The `tests` job also holds the repository hygiene gates, all of them before its
Go tests: `gofmt` over every tracked Go file outside `testdata`, whose lint
fixtures are deliberately unformatted; `go mod tidy -diff` and
`go mod verify` in `.`, `runner/lint`, and `runner/tools` (not `runner`, whose
manifest `dagger develop` rewrites), and `ruff check` over `scripts` and
`sdk/patched-go`; and `scripts/test-doc-pins`, which requires every consumer
example to pin the newest release in `CHANGELOG.md`. The Go test step writes a coverage profile that is uploaded
as an artifact for seven days; no threshold gates the run.

`security.yml` runs beside it: `zizmor` over the workflows and composite
actions on every pull request, push to `main`, and weekly, failing on findings
of medium severity and above; OpenSSF Scorecard with a SARIF upload to code
scanning on `main` and the weekly schedule, since Scorecard reads the default
branch rather than a pull request's merge ref; and `dependency-review` on pull
requests, failing on high severity. `dependency-review` needs the repository's
**Dependency graph**, which is a repository setting (Settings → Code security)
and not something a workflow can enable. The job checks for it first: without
it the review is skipped with a note in the job summary, and with it the review
is a real gate. This repository has it turned off today, so dependency review
is a no-op until an admin enables the dependency graph.

`release.yml` publishes the archives, their SBOMs, `checksums.txt`, and a build
provenance attestation when a `vX.Y.Z` tag is pushed; see
[release archives](releases.md).

Event → `./verify` mapping:

- Draft PR / push to `main`: `lint` runs `branch`; `tests` skips Dagger verify (lint already covered static checks).
- Ready PR / merge queue: `lint` runs `branch`; `tests` runs `self-test` (together equivalent to former `pre-merge`).
- Daily schedule (07:23 UTC): `lint` runs `branch`; `tests` runs `main` (fresh audit + `go-vuln`).
- Manual dispatch: `lint` runs `branch`; `tests` runs the requested run (default `self-test`, since `lint` already covers the static checks in `pre-merge`).

### Required checks (branch protection)

| When | Require |
| --- | --- |
| Early PR progress (including drafts) | **`lint`** |
| Merge / ready-for-review / merge queue / `main` | **`lint`**, **`tests`**, **`language-contracts`**, **`action`**, **`release-smoke`** |
| Pull requests | `semantic-lint` (advisory; not required to pass; the job passes with no findings when `TYPESAFE_API_KEY` is absent) |

Do **not** make draft progress wait on `release-smoke` or full `tests`. When adopting this workflow, replace any required check named `verify` with `lint` and `tests` the same day.

GitHub enforces these from a repository ruleset, which lives in repository settings rather than in a file: rules a pull request could edit would let that pull request weaken them. `.github/rulesets/main.json` is the reviewed copy, and `scripts/test-rulesets --live` in the `tests` job keeps it honest:

- every required check must name a job in `.github/workflows` (its `name:` if it has one, otherwise its ID), so renaming or relabeling a required job fails the pull request instead of leaving later merges waiting on a check that never reports;
- the rulesets GitHub enforces must match the committed copies. The run's read-only token cannot see `bypass_actors`, so CI compares them only when an admin runs the script locally.

To change a rule, edit it in **Settings → Rules → Rulesets**, then export the new state in the same pull request that needs it:

```sh
gh api repos/wangjohn/levenshtein/rulesets/<id> \
  | jq '{name, target, enforcement, conditions, bypass_actors, rules}' > .github/rulesets/main.json
```

To rename a required job, rename it and update `.github/rulesets/main.json` in one pull request. Its drift check fails until an admin changes the live ruleset to match; do that just before merging, then rerun `tests`.

Treat warm lint wall time creeping toward warm tests as a CI performance regression. Read step and job durations from the Actions run view and compare medians across warm `ubuntu-24.04` runs.

### Result-cache trust

- Restore `verification-v1-lint` / `verification-v1-tests` on every event, and save on every event too. A pull request run saves into its own merge-ref scope, which only reruns of that PR can restore and `main` never reads, so a fork run cannot seed `main`'s entries.
- Lint and tests use **separate** verification keys so they cannot race one entry. The generated SDK still uses an exact key with no `restore-keys`.
- `merge_group` is not one of the events that writes the default-branch cache scope, so its saves are invisible to `main`; the tests scope on `main` is seeded by schedule and `workflow_dispatch` runs. The other direction is open: a pull request, including one from a fork, can restore `main`'s entries. They hold verification results and generated code, never secrets.
- Entries are small JSON records, and the SDK entry is touched on every run, so result entries do not push it out of the repository's 10 GB Actions cache budget.
- Scheduled `main` keeps `rerun_checks: true`; `go-vuln` always re-executes.

### Caches and self-config notes

The `lint` and `tests` jobs (and `vulnerabilities`) restore a pinned Dagger CLI from the Actions cache when `.dagger-version` / `scripts/dagger-checksums.txt` are unchanged; install falls back to download on miss. That block and the generated-SDK restore/generate/save block each live in one composite action (`.github/actions/setup-dagger`, `.github/actions/dagger-sdk`) rather than being repeated per job. `language-contracts` uses the setup-go module cache over root `go.sum`.

Generated SDK (`runner/dagger.gen.go`, `runner/internal/dagger`, `runner/internal/telemetry`) uses **exact** key `dagger-sdk-v2-…` (no `restore-keys`). Restore + save are separate steps; save runs only when `scripts/ci-dagger-sdk` reports `ready=true` after a miss. Readiness is decided by compiling: a restored SDK is usable exactly when `go build ./...` succeeds in `runner`, which needs no Dagger session. Anything that fails to compile — including a truncated or poisoned entry — is regenerated, and the script `mkdir`s `internal/telemetry` so the cache save still finds every path when codegen emits no sources there. Bump the `vN` prefix to abandon a stuck key. Warm check: Generate reports a reused SDK instead of running `dagger develop`. Invalidate when any of these change: `dagger.json`, `.dagger-version`, `scripts/dagger-checksums.txt`, `runner/go.mod`, `runner/go.sum`, `runner/toolchain.json`, `runner/*.go`, `sdk/patched-go/**`. `vulnerabilities.yml` still always runs `scripts/test-sdk-security`.

Completed verification results are restored into `$RUNNER_TEMP/levenshtein-verification-v1` and passed to `./verify --cache-dir` on `lint` / `tests` with per-job keys (`verification-v1-lint-${{ runner.os }}-<sha>`, `verification-v1-tests-${{ runner.os }}-<sha>`). The key is deliberately per-commit with a prefix `restore-keys` fallback: the CLI already fingerprints each target, so handing it the newest earlier directory lets it reuse the entries that are still valid instead of missing the whole cache on any source edit. Save runs on every event and on failed runs, since results are keyed by content and a pull request's cache is scoped by GitHub to that PR and its base branch. Warm check: a rerun of the same commit hits the exact key and skips Save; a new commit reports a `restore-keys` match.

In-engine Go module/build and Staticcheck `CacheVolume`s remain version-keyed in `runner/` but are **session-local** on ephemeral GitHub-hosted runners. Persisting those volumes across VMs is **blocked** for Dagger **0.21.9** (no supported CI export/restore API without experimental hacks).

Self-config targets use narrow literal `inputs` (not `"."`): root Go module paths, `runner` / `runner/lint`, and `.github/workflows` for workflow-lint. Doc-only edits therefore do not invalidate Go analysis result fingerprints. The one exception is the `repository` target, which keeps `"."` so `semantic-lint` still judges Markdown and workflow changes. Independent checks in a run execute concurrently (bounded workers) inside `./verify`.

### Success criteria and gaps

| Criterion | Status |
| --- | --- |
| Lint ≪ tests (warm lint well under 1 min) | In progress — needs warm SDK and result-cache hits; in-engine build/cache volumes cannot yet persist across ephemeral runners |
| Coverage preserved on ready/merge/`main`/schedule | Met by job split + event mapping |
| Result-cache isolation between untrusted PRs and `main` | Met (PR-written caches stay in the PR's merge-ref scope, which `main` never reads) |
| Freshness (`rerun_checks` / `go-vuln`) | Met |
| Self-CI scope (not consumer packaging) | Met |

### Out of scope here

Consumer CLI `--source` cache UX, install/release packaging, and Swift macOS lint jobs for native application repos are tracked in other workstreams (consumer CLI performance, setup packaging, Go–Swift lints), not in this self-CI workflow.

Pre-split baseline (monolithic `verify` ~4.6–5 min on `ubuntu-24.04`; `dagger develop` ~93s; `./verify` ~94–114s): Actions run [35283983398](https://github.com/wangjohn/levenshtein/actions/runs/35283983398).

## Pinned dependencies

Stable versions checked on September 15, 2026:

| Dependency | Version | Pin |
| --- | --- | --- |
| Go for lint and local development | 1.27.1 | `.go-version`, root and `runner` `go.mod`, fixture modules, `runner/toolchain.json` |
| Go container | 1.27.1 on Debian Trixie | Tag and immutable image digest in `runner/toolchain.json` |
| Dagger CLI / engine / SDK | 0.21.9 | `.dagger-version`, `dagger.json`, root `go.mod`, generated module dependencies |
| Staticcheck | 2026.2.1 (`honnef.co/go/tools` v0.8.1) | `runner/toolchain.json` |
| Actions checkout / setup-go / cache | 7.0.1 / 7.0.0 / 6.1.0 | Full commit hashes in the workflows and composite actions |

The pinned Go version builds the CLI (the launcher provisions it when the host Go differs) and is what runner development and unit tests expect. The actual lint runs on Linux with default build tags, using the pinned container toolchain with automatic Go toolchain switching disabled. A temporary SDK adapter fixes Dagger 0.21.9’s forced logging dependency overrides for both generation and execution; see [dependency security](dependencies.md#dagger-wrapper-dependency-security). The wrapper’s Go language version must stay at or below the Go version of the codegen container (`goImage`), which Dagger’s module generator refuses to exceed; it does not restrict the Go version of repositories being checked. `go.sum` records checksums. Upgrade pins together and validate the fixtures before adoption.

## Develop the shared checks

Use Go 1.27.1 and the pinned Dagger CLI:

```sh
dagger develop --compat=skip
GOTOOLCHAIN=local go test -race ./...
(cd runner && GOTOOLCHAIN=local dagger run go test ./...)
./verify pre-merge
./scripts/test-consumers
```

The generated Go SDK needs a Dagger session, including during unit tests. The deliberately broken Go module lives under `runner/testdata`, outside ordinary test discovery. The self-test requires good code, vendored dependencies, and embedded templates to pass, bad code to emit each intended rule, and broken/empty modules to fail verification. A compiler failure cannot substitute for an expected lint finding.

`scripts/test-consumers` checks module and workspace vendoring through the launcher. It also adds synthetic private env files next to root and nested `.env.example` templates; each embed must match exactly one file, proving the templates survive filtering and the private files do not. A final case verifies undeclared dependencies still fail without vendoring.

### Initial measurements (original Dagger-only launcher)

On an Intel Mac with a two-CPU, 4 GiB Colima VM, the first `pre-merge` run after SDK setup took **136 seconds**, including the Go image download and Staticcheck compilation. A repeat took **2.5 seconds**; a fresh `main` audit took **14 seconds**. A single fixture check took **1.8 seconds**. These are small-pilot measurements, not guarantees for application repos; initial CLI/VM installation and SDK setup are excluded.

Before another repo adopts this setup, check out an explicit Levenshtein commit and invoke its launcher with `--source`. [Release archives](releases.md) package the CLI and shared checks together; automated publication and version-update PRs are later work.

## Standalone planning and configuration

The `./verify` launcher now builds a standalone Go CLI. Planning and configuration validation work without Dagger; execution of Go checks still uses the pinned Dagger module. The existing configuration remains supported, and [version 1 configuration](configuration.md) adds named targets, checks, environments, and explicit run freshness.

## Dagger integration

The wrapper uses the official Go SDK and one engine session per run. It loads the pinned shared module once and passes each consumer directory, module path, and freshness token as function arguments. Dagger owns container execution, dependency downloads, compilation caches, and execution caching. A fresh audit reruns analysis while retaining download and compiler caches.

`GoLint` and `SelfTest` are also native Dagger checks: `dagger check -l` lists them and `dagger check` runs them against this checkout. The wrapper calls the same functions with explicit consumer inputs; named-run selection lives only in the standalone CLI. The former Dagger `verify` and `check` functions have been removed. Use `./verify` for Levenshtein runs.

Shared module identity must remain separate from consumer inputs. Loading a synthetic module directory containing consumer files would change Dagger's cache namespace on every source edit or fresh audit. See [dependency choices](dependencies.md).
