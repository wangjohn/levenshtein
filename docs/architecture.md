# Architecture

This describes how the CLI actually works, for anyone changing it. See
[setup](setup.md) and [configuration](configuration.md) for user-facing
instructions.

## The `./verify` launcher

`./verify` builds and runs the standalone Go CLI in `cmd/levenshtein`
(`main.go`, `args.go`). It parses flags and an optional run name (default
`branch`), loads `levenshtein.json` from the source repository, and builds a
plan for that run.

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
  `workflow-lint`, `self-test`, `command`, `semantic-lint`) bound to a target
  and environment, plus kind-specific options for `command` and
  `semantic-lint` checks (see [configuration](configuration.md)).
  `internal/verify/validation.go` enforces which options apply to which kind.
- **Run**: a named list of check IDs plus `rerun_checks`, which forces fresh
  verification (bypassing verdict caches) while keeping compatible
  dependency/build caches.

A repository without `levenshtein.json` gets built-in single-module Go
defaults (`Load` in `config.go`).

## The planner

`internal/verify/plan.go` resolves a run's check IDs against `Config` into a
`Plan`: it validates that every target/environment/check reference exists,
checks executor-specific option rules, confirms target directories exist
under the source tree, and (for Dagger checks) validates the declared inputs
can be imported (`daggerIncludes`). None of this starts Docker, Dagger, or any
executor, so `--dry-run` needs no container runtime and none of the tools
the checks themselves use. The launcher still needs the pinned Go.

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

`internal/verify/native.go` runs `command` and `semantic-lint` checks as
trusted host processes (macOS or Linux only). There is no sandbox, so native
commands have full host access. Before running, `validateTools` executes each
`Environment.Tool`'s version command and compares its trimmed stdout against
the pinned `Tool.Version`. For `command` checks, `internal/verify/stages.go`
then runs any declared `preparation` and `build` stages (in that order), each
with its own cache key derived from its inputs, environment, and (for build)
the preparation it depends on. A stage is skipped when its declared outputs
still match a recorded run, which requires the environment to declare an
`identity`. `semantic-lint` checks accept no stages.

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
eligible check (Dagger checks, or native checks with `cache: true`), it:

1. Computes a fingerprint (`internal/verify/fingerprint.go`): a content hash
   over the target's declared inputs plus any stage inputs, the shared
   implementation (root `go.mod`/`go.sum`/`cmd`/`internal`, and for Dagger
   checks also `.dagger-version`, `dagger.json`, `runner`, `sdk`), the check
   definition itself, `runtime.GOOS`/`GOARCH`, and (for native checks) the
   resolved environment variables.
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
  analyzers listed in [Go lint rules](go-lint.md), and the house rules
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
