# Architecture

This describes how the CLI actually works, for anyone changing it. See
[setup](setup.md) and [configuration](configuration.md) for user-facing
instructions.

## The `./verify` launcher

`./verify` builds and runs the standalone Go CLI in `cmd/levenshtein`
(`main.go`, `args.go`). The shell launcher exports `GOWORK=off` and
`GOTOOLCHAIN=go<version>` from `.go-version`, so any host `go` builds the CLI
with the pinned toolchain, downloading it once if it is not installed. It
parses flags and an optional run name (default `branch`), loads
`levenshtein.json` from the source repository, and builds a plan for that run.

With `--dry-run`, it prints the plan and exits; no executor runs. Otherwise it
builds a `Dagger` executor and a `Native` executor, wraps both in
`CachedExecutor`, executes the plan, and prints a JSON report. Exit codes:

- `0`: every selected check passed.
- `1`: planning succeeded but a check failed, errored, or was cancelled/incomplete.
- `2`: argument parsing, configuration loading, or planning failed. Also no
  shared checkout (neither `--shared` nor `LEVENSHTEIN_SHARED_ROOT`), a
  `--cache-dir` inside the source or shared checkout, or a failure to write
  the plan or report. `--help` exits `0`.

## Configuration concepts

`internal/verify/config.go` defines the version 1 schema:

- **Target**: a working directory (`dir`), a `workspace` context, and declared
  `inputs` (literal paths, not globs) used for both Dagger source import and
  cache fingerprinting.
- **Environment**: an `executor` (`dagger` or `native`), plus native-only
  options such as `identity`, `env`, `pass_env`, and pinned `tools`.
- **Check**: a `kind` (`go-lint`, `go-vet`, `go-http`, `go-sql`, `go-vuln`,
  `workflow-lint`, `self-test`, `command`, `semantic-lint`) bound to an
  environment and to either one `target` or a list of `targets`, plus
  kind-specific options for `command` and `semantic-lint` checks (see
  [configuration](configuration.md)). `internal/verify/validation.go` enforces
  which options apply to which kind.
- **Run**: a named list of check IDs plus `rerun_checks`, which forces fresh
  verification (bypassing verdict caches) while keeping compatible
  dependency/build caches.

A repository without `levenshtein.json` gets built-in single-module Go
defaults (`Load` in `config.go`).

## The planner

`internal/verify/plan.go` resolves a run's check references against `Config`
into a `Plan`. `selections` turns each reference into the checks it selects: a
check declared with `targets` expands to one planned check per target, with the
ID `<id>/<target>`, and a run may instead name a single `<id>/<target>`.
`planCheck` then validates that every target/environment reference exists,
checks executor-specific option rules, confirms target directories exist
under the source tree, and (for Dagger checks) validates the declared inputs
can be imported (`daggerIncludes`). None of this starts Docker, Dagger, or any
executor, so `--dry-run` needs no container runtime and none of the tools
the checks themselves use. The launcher still builds the CLI with the pinned
Go, which it provisions through `GOTOOLCHAIN` when the host version differs.

## Executors

`internal/verify/run.go` defines the `Executor` interface
(`Execute(context.Context, Request) Result`) and runs a plan's checks
concurrently, bounded by the smaller of `maxCheckParallelism` and `GOMAXPROCS`,
serializing checks that share
a preparation stage.

### Dagger executor

`internal/verify/dagger.go` holds one Dagger SDK session (`dagger.Client`)
per CLI invocation and serves the pinned module in `runner/` once
(`client.ModuleSource(shared).AsModule().Serve`). Each check calls a
function on that session (`goLint`, `selfTest`, or `sharedCheck` for
vet/HTTP/SQL/vuln/workflow-lint) with a freshness nonce, plus the consumer
source directory and module path for every kind except `selfTest`;
`sharedCheck` also receives the check kind. Consumer inputs travel as
arguments, so they never become part of the module's own identity or cache
namespace.

The Dagger module itself lives in `runner/` (`runner/main.go`,
`runner/checks.go`) and must be its own Go module (`runner/go.mod`) because
Dagger's `go generate`/`dagger develop` step manages that module's
dependencies independently of the root CLI. Failures surface as GraphQL
errors; when Staticcheck or another check reports diagnostics, `runner`
attaches them as a `levenshteinFindings` extension on a `gqlerror.Error`, and
`daggerResult` in `dagger.go` turns that into a `Result{Status: StatusFailed}`
with the findings in `Details`. A GraphQL error without that extension, or
whose extension does not decode to a non-empty list, becomes `StatusError`
instead.

### Native executor

`internal/verify/native.go` runs `command`, `semantic-lint`, and the shared Go
kinds `go-lint`, `go-vet`, `workflow-lint` and `go-vuln` as trusted host
processes (macOS or Linux only). There is no sandbox, so native
commands have full host access. Before running, `validateTools` executes each
`Environment.Tool`'s version command and compares its trimmed stdout against
the pinned `Tool.Version`. For `command` checks, `internal/verify/stages.go`
then runs any declared `preparation` and `build` stages (in that order), each
with its own cache key derived from its inputs, environment, and (for build)
the preparation it depends on. A stage is skipped when its declared outputs
still match a recorded run, which requires the environment to declare an
`identity`. `semantic-lint` checks accept no stages.

#### Shared Go kinds on the native executor

`internal/verify/kinds.go` registers `go-lint`, `go-vet`, `workflow-lint` and
`go-vuln` for the native executor as well as the Dagger one; `self-test`,
`go-http` and `go-sql` stay Dagger-only. `internal/verify/gotools.go` builds the
helper binaries (`levenshtein-lint` from `runner/lint`, `actionlint` and
`govulncheck` from `runner/tools`) out of the pinned shared checkout into
`cache.Dir/tools/` with `GOWORK=off GOTOOLCHAIN=local go build -trimpath`,
serialized by a lock in `cache.Dir/locks`; Go's own build cache makes a repeat
build cheap, so there is no staleness logic. `internal/verify/gochecks.go` then
runs each check in the target directory with the same preflight the container
does (`go list ./...`, refusing a module with no packages), points Staticcheck
at `cache.Dir/staticcheck` — or a throwaway directory on a fresh run, mirroring
the runner's nonce — and reads the rule list from the shared checkout's
`runner/toolchain.json` rather than repeating it. Every Go invocation runs
with `GOTOOLCHAIN=local` and `GOWORK` set to what the container would see:
the nearest declared `go.work` between the target directory and the source
root (`workspace` in `gochecks.go`), or `off`. Without a cache directory, a
check's tools go into a temporary directory removed when it finishes.

`internal/verify/findings.go` is a deliberate copy of the runner's
`parseFindings`/`commandFindings` and of its check-selection filter
(`allowed`/`selects`), since `runner` is a separate `package main` module
that cannot be imported. Both filters are tested against one table,
`runner/testdata/selection.json`, so they cannot drift apart unnoticed. It
produces the same `{"findings": [...]}` `Details` envelope as `daggerResult`,
with locations relative to the source root, so a report does not say which
executor produced it. Because the host's Go is not covered by any snapshot,
`fingerprint` adds its `go env GOVERSION GOOS GOARCH` to the cache key for
these kinds only, and the shared implementation snapshot covers `runner/` for
them on either executor.

`internal/verify/command.go` builds the actual `os/exec.Cmd` with a minimal
inherited environment (`PATH`, `HOME`, `TMPDIR`, `TMP`, `TEMP`, `SystemRoot`,
plus explicit `pass_env`/`env` entries), defaults `LANG` to `C` unless
configuration overrides it, and adds `LEVENSHTEIN_SOURCE`,
`LEVENSHTEIN_WORKSPACE`, and `LEVENSHTEIN_RERUN_CHECKS`. `process_unix.go`
puts the child in its own process group and kills the whole group
(`SIGKILL` to `-pid`) on timeout or cancellation, so subprocesses cannot
outlive a killed check.

## Result cache

`internal/verify/cache.go` wraps an `Executor` in `CachedExecutor`. For an
eligible check (Dagger checks, the shared Go kinds on either executor, or
native `command` checks with `cache: true`), it:

1. Computes a fingerprint (`internal/verify/fingerprint.go`): a content hash
   over the target's declared inputs plus any stage inputs, the shared
   implementation (root `go.mod`/`go.sum`/`cmd`/`internal`, for Dagger
   checks also `.dagger-version`, `dagger.json`, `runner`, `sdk`, and for
   native shared Go checks also `runner`), the check
   definition itself, `runtime.GOOS`/`GOARCH`, (for native checks) the
   resolved environment variables, and (for native shared Go checks only) the
   host toolchain's `go env GOVERSION GOOS GOARCH`.
2. Takes a per-key file lock (`internal/verify/lock.go`, backed by
   `gofrs/flock`) so concurrent processes do not race the same cache entry.
   Native checks take an advisory per-source workspace lock first, whether or
   not the check is cacheable, so processes sharing a cache directory do not
   mutate one checkout at the same time.
3. On a hit, restores the recorded result and any declared `Artifacts` from
   `cache.Dir/results/<key>.json` (atomic, rejects symlinked destinations).
4. On a miss or `rerun_checks`, executes the check, and on success saves the
   result and artifacts back under that key, clearing a `.retry` marker.
5. If a check starts but the process is interrupted before a successful save,
   the `.retry` marker left behind forces the next attempt to bypass any
   cached verdict, even if the fingerprint would otherwise hit.

`go-vuln` is never cached: `CachedExecutor.Execute` forces `RerunChecks` and
returns `CacheStatus: "disabled"` for it unconditionally, because
vulnerability data changes independently of source fingerprints.

## Report and exit codes

`internal/verify/run.go`'s `Execute` returns a `Report` with the resolved
`Plan` and one `Result` per check (status, timing, stdout/stderr, cache info,
stage results, and any `Details`). The CLI in `cmd/levenshtein/main.go`
encodes that report as JSON to stdout, then maps `Report.Status` to the
process exit code described above.

## The four Go modules

- **Root module** (`go.mod`, module `github.com/wangjohn/levenshtein`): the
  `cmd/levenshtein` CLI and `internal/verify` planning/execution/cache logic.
- **`runner/`** (`runner/go.mod`, module `dagger/levenshtein`): the Dagger
  module that implements the actual Go checks in containers. It must be a
  separate module because Dagger generates and manages its own SDK code and
  dependency graph here, independent of the root CLI's dependencies.
- **`runner/lint/`** (`runner/lint/go.mod`): an isolated module for the
  Staticcheck-based lint binary (`levenshtein-lint`), so its analyzer
  dependencies (Staticcheck's whole analyzer set, the curated upstream
  analyzers listed in [Go lint rules](checks.md#go-lint-rules), and the house rules
  `LV1001`-`LV1005` in `runner/lint/policy`) do not leak into the Dagger
  module's own dependency resolution.
- **`runner/tools/`** (`runner/tools/go.mod`): pins `actionlint` and
  `govulncheck` via Go's `tool` directive, so their versions are locked
  independently of the modules that build and run them.

## `sdk/patched-go`

`sdk/patched-go` is a temporary, Python-implemented Dagger Go SDK generator
that forces the Dagger 0.21.9 module SDK's OpenTelemetry logging dependencies
to a version without [GO-2026-4985](https://pkg.go.dev/vuln/GO-2026-4985),
since editing `runner/go.mod` directly cannot survive Dagger's own module
regeneration. See [dependency security](dependencies.md#dagger-wrapper-dependency-security)
and `sdk/patched-go/README.md` for how to validate and eventually remove it.

## Design contracts

The sections above describe what the CLI does today. These are the contracts a
new check implementation or cache layer has to satisfy, whatever language it
serves. [The roadmap](roadmap.md) covers forward-looking expansion.

### Language adapter contract

An adapter is an internal check implementation using the common planner,
executors, and results. Keep the target/check/environment/run model; do not
assign a single language to an entire repo. Each adapter defines:

- **Workspace inputs:** distinguish the execution directory from the workspace
  root; include relevant root configuration, local dependencies, shared
  fixtures, generated inputs, and test configuration.
- **Execution variants:** include selected packages/tests, toolchains,
  dependency selection, and language-specific options in the check identity and
  reported scope. Record resolved versions, not just a requested version range.
- **Shared preparation:** declare preparation inputs, reusable outputs,
  compatibility, and read/write access. Lint, type checking, and tests can share
  compatible setup. Isolate environments with different dependency selections
  and serialize conflicting mutations; a generic command may keep setup embedded
  until it can declare a safe reusable boundary.
- **Results and freshness:** preserve native output, collect required artifacts,
  and define how fresh verification bypasses verdict reuse while retaining
  compatible setup/build artifacts.

#### Rust and Python constraints

- **Rust workspaces:** Cargo shares a root lockfile and build directory. Include
  workspace configuration and relevant path dependencies even when checking one
  member. Features, profiles, selected packages, and target platforms identify
  different verification scopes. See [Cargo workspaces](https://doc.rust-lang.org/cargo/reference/workspaces.html)
  and [test options](https://doc.rust-lang.org/cargo/commands/cargo-test.html).
- **Rust build inputs:** account for `build.rs`, generated sources, environment
  inputs, native libraries, and required system tools. Declaring only Rust
  source files is insufficient; see [Cargo build scripts](https://doc.rust-lang.org/cargo/reference/build-scripts.html).
- **Python environments:** pin the interpreter and resolved dependencies;
  include groups/extras, test plugins, configuration, and `conftest.py`/shared
  fixtures. A uv recipe is one option, not a requirement for every consumer.
  Reuse compatible preparation across checks; see [uv synchronization](https://docs.astral.sh/uv/concepts/projects/sync/).
- **Python cache portability:** reuse compatible downloaded/built packages
  across workers. Reuse a complete virtual environment only when interpreter,
  platform, dependency selection, and filesystem layout match; otherwise
  recreate it from cached dependencies. Virtual environments contain absolute
  interpreter paths and are generally nonportable. Local package builds and
  dynamic metadata need their actual inputs reflected in underlying tool caches
  too. See [Python virtual environments](https://docs.python.org/3/library/venv.html)
  and [uv cache inputs](https://docs.astral.sh/uv/concepts/cache/).
- **Python fresh runs:** pytest's cache records state such as previous failures,
  not reusable passing-suite verdicts. A fresh full-suite audit must execute the
  configured suite; `--last-failed` alone cannot satisfy it. Dependency caches
  can remain warm. See [pytest cache behavior](https://docs.pytest.org/en/stable/how-to/cache.html).

The small Rust/Python fixtures in [language fixtures](language-fixtures.md)
validate these boundaries before shared language recipes expand.

### Cache contracts and invalidation

Default ordinary runs to aggressive reuse, including successful check results. A
reusable check implementation must define its input fingerprint, reusable
outputs, environment requirements, and fresh-execution behavior. Native command
checks participate in the same contract as container checks.

#### Separate cache layers

Each layer has its own key and compatibility rules. Do not key all preparation
on the complete check fingerprint.

| Layer | Inputs that determine reuse |
| --- | --- |
| Tool/dependency preparation | Resolved toolchain, platform, manifests/lockfiles, dependency groups/extras, installer configuration, preparation implementation, and any local package/build inputs it consumes |
| Compilation/build artifacts | Relevant source and dependencies, compiler/system tools, platform, build flags/profile/features, build scripts, and generated inputs |
| Completed verification | Complete relevant source/test/configuration inputs, resolved environment, check implementation/options, and selected scope |

For example, editing a Python test should invalidate its verification result
while retaining an unchanged dependency environment. Rust source edits
invalidate dependent results while Cargo reuses compatible build artifacts.
Include source in preparation keys when preparation consumes it; local package
builds are not determined by lockfiles alone. A check may omit layers it does
not need.

#### What identifies reusable work

An exact result key includes the check/target identity and input scope,
effective command and options, source and test content, fixtures and
configuration, dependency manifests/lockfiles and relevant local dependencies,
invoked scripts and helpers, effective shared check/executor implementation, and
environment/toolchain identity. Include platform/architecture, container image
digest or native SDK/compiler identity, build flags, relevant declared
environment values, and any base revision/diff that the check actually consumes.
Never put raw credentials in cache metadata; credential-dependent results
require an appropriate version/state identity or fresh execution.

Use content fingerprints so unchanged work can be reused across commits.
Preserve the source and pinned shared revisions as provenance. Hash relevant
shared implementations and their dependencies rather than invalidating every
check for an unrelated documentation or recipe change. Input discovery must
account for untracked inputs, deletions, generated sources, workspace
replacements, and shared contracts.

For generic repo-owned commands, start with the full declared source scope,
scripts, arguments, and environment. Broaden uncertain dependency scopes, then
narrow them with evidence. A tool that reads live services or other unbounded
state declares fresh execution unless that state has a dependable identity. It
still benefits from aggressive tool, dependency, and compilation reuse.

#### Reuse across processes and workers

- Restore only exact completed-result keys. Prefix/fallback restoration is for
  dependency/build caches whose tools validate compatibility before reuse.
- Preserve result output and required artifacts together. A missing or invalid
  artifact turns the lookup into a miss; use atomic publication after successful
  completion. Record original execution time and cache provenance rather than
  presenting a reused result as freshly executed.
- Persist caches across agents and CI jobs using supported existing storage or
  persistent workers. Scope mutable outputs by environment/check/worktree and
  serialize conflicting writers; share immutable artifacts where compatible.
  Reuse preparation and coalesce identical work where supported.
- Keep trusted result publication separate from untrusted pull-request writes.
  Shared dependency/artifact reuse must respect repository and access
  boundaries. Cache backend failure should fall back to recomputation when
  execution is available.
- Start executors lazily. A complete result hit should avoid container startup,
  tool installation, and check execution. Missing required work without a
  suitable environment leaves the run incomplete.

#### Freshness and evidence

A fresh run bypasses completed-result reuse at every verification layer,
including Dagger execution and native test/analysis caches. Compatible tool
installations, downloads, compiled libraries, and test binaries remain reusable.
Each check adapter must prove that its fresh mode actually executes
verification; a new outer cache key alone is insufficient. Freshly executed
successes may populate later ordinary-run caches.

Validate both hits and misses: unchanged work hits; relevant source, test,
fixture, dependency, script, configuration, toolchain, and shared implementation
changes miss; unrelated target changes retain valid hits. Exercise persisted
caches from another process/worker, incomplete/corrupt entries, and a fresh
audit after a cached success.

Measure cold, warm, small-edit, and fresh-audit runs. Separate
hashing/lookup/restore, environment startup, setup, compilation, and check
execution; record cache hit rate, work avoided, and median/tail latency. If
restoring an artifact costs more than recomputing it, adjust cache granularity
or retention. Aggressive caching must reduce end-to-end latency.
