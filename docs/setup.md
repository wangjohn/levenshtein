# Setup and usage

Shared Go checks run pinned correctness, error handling, enum, resource, workflow, and vulnerability tools in Dagger. The runner accepts a source checkout and a named run; your existing CI supplies workers and decides when to invoke it. See [use from an application repo](consumer-ci.md) for local and CI examples. Native commands and local result/setup/build caching are supported. Consumer adoption and cross-worker cache transport are the next steps in the [implementation plan](implementation.md).

## Prerequisites

The source launcher requires Go **1.27.1**. Go checks require a running Docker-compatible container runtime. The Go SDK downloads and checksum-verifies Dagger **0.21.9** automatically. Native commands and planning do not start Dagger.

To develop the shared Dagger module or use `dagger check` directly, install the pinned CLI with the checked-in archive checksums:

```sh
./scripts/install-dagger
export PATH="$HOME/.local/bin:$PATH"
dagger version
```

The installer supports macOS Intel/Apple Silicon and Linux amd64. On macOS, one option for the container runtime is Colima:

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

A single Go module at the source root works without configuration. For multiple modules or custom runs, add `levenshtein.json` to that repo.

Use the [version 1 consumer example](consumer-ci.md#the-same-command-locally-and-in-ci) for explicit product targets, checks, and run selections. `inputs` restricts Dagger's imported source as well as its cache scope; include required manifests, local dependencies, and fixtures. Native command inputs only describe cache scope and do not restrict host access. See [source boundaries](configuration.md#source-boundaries).

A configuration file replaces defaults. Paths are relative to the source root. Every selected check runs or reuses an eligible result; change-based selection is not implemented. Available shared kinds are listed in [Go lint rules](go-lint.md). Legacy `modules` configuration remains supported, but new consumers should use version 1.

Version 1 runs use explicit `rerun_checks: true` for fresh audits, regardless of their name. Levenshtein's own `main` is configured that way. Audits bypass passing-verdict reuse while retaining compatible downloads and compiler caches. Vulnerability scans always execute against current advisory data. Add new checks explicitly to your configured full run during this pilot.

## Levenshtein's own CI

The checked-in `Levenshtein self-checks` workflow verifies this repo's runner and fixtures. Its cron schedules that verification only. Application repos call the shared runner from their own CI, as shown in the [consumer guide](consumer-ci.md).

For Levenshtein itself, the workflow selects:

- `branch` for draft PR updates and pushes to `main`.
- `pre-merge` for ready PRs and merge-queue candidates.
- `main` daily at 07:23 UTC, using the default branch.
- A configurable run for manual dispatch.

This workflow also runs the runner's Go unit tests, consumer regression fixtures, and verifies that the bad fixture fails with all three intended diagnostics. Require `verify`, `release-smoke`, and `language-contracts` for Levenshtein once they have run; `language-contracts` exercises the Rust and Python contracts. Application repos maintain their own merge gates and schedules.

## Pinned dependencies

Stable versions checked on September 15, 2026:

| Dependency | Version | Pin |
| --- | --- | --- |
| Go for lint and local development | 1.27.1 | `.go-version`, fixture modules, `runner/toolchain.json` |
| Go language version of the Dagger wrapper | 1.26.7 | `runner/go.mod` |
| Go container | 1.27.1 on Debian Trixie | Tag and immutable image digest in `runner/toolchain.json` |
| Dagger CLI / engine / SDK | 0.21.9 | `.dagger-version`, `dagger.json`, root `go.mod`, generated module dependencies |
| Staticcheck | 2026.2.1 (`honnef.co/go/tools` v0.8.1) | `runner/toolchain.json` |
| Actions checkout / setup-go | 7.0.1 / 7.0.0 | Full commit hashes in the workflow |

The host Go version is needed by the source launcher, runner development, and unit tests. The actual lint runs on Linux with default build tags, using the pinned container toolchain with automatic Go toolchain switching disabled. A temporary SDK adapter fixes Dagger 0.21.9’s forced logging dependency overrides for both generation and execution; see [dependency security](dependencies.md#dagger-wrapper-dependency-security). The wrapper’s Go language version does not restrict the Go version of repositories being checked. `go.sum` records checksums. Upgrade pins together and validate the fixtures before adoption.

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
