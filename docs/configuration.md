# Standalone runner configuration

`./verify [RUN] --source /path/to/repo` builds the small Go CLI through Go's incremental build cache and runs it. The launcher requires the version in `.go-version`; Dagger is needed only when a selected Dagger check executes. A prebuilt CLI can be used directly with `--shared /path/to/pinned/levenshtein`.

`./verify branch --dry-run` reads configuration and returns the selected plan without starting execution tools. Relative target and input paths are resolved from the source repository. Inputs are literal files/directories, not glob patterns. Workspace context can differ from the check's working directory. Target/workspace directories must exist inside the source repository.

## Version 1

```json
{
  "version": 1,
  "targets": {
    "api": {"dir": "services/api", "workspace": ".", "inputs": ["services/api", "go.work", "contracts"]}
  },
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "cleanup": {"kind": "go-lint", "target": "api", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["cleanup"]},
    "pre-merge": {"checks": ["cleanup"]},
    "daily": {"checks": ["cleanup"], "fresh": true}
  }
}
```

Supported checks are `go-lint` and Levenshtein's own `self-test` using Dagger, plus native `command` checks. Go tool versions remain pinned in the shared checkout. Local caching is described below.

A run selects check IDs. `fresh: true` forces verification execution while retaining compatible dependency/build caches. Any run name can use it; versioned configuration gives `main` no special behavior. Unknown checks, executors, references, and configuration fields fail explicitly.

Legacy `modules` and array-valued `runs` remain supported. They translate into Go targets/checks, and legacy `main` retains fresh behavior. A source without configuration still receives the original single-module Go defaults.

## Results

The CLI emits a versioned JSON report with the resolved plan and a result for every selected check. Results distinguish `passed`, `failed`, `error`, `cancelled`, and `incomplete`, with timing and native output/details. A run succeeds only when every selected check passes. Planning/configuration errors exit 2; unsuccessful verification exits 1.

Application repos retain their own CI triggers, workers, schedules, and merge gates. Pin the shared checkout as described in [consumer CI](consumer-ci.md).

## Native commands

Native commands execute on the supplied macOS or Linux worker. They are trusted repo code, not a sandbox. Levenshtein does not provision Xcode or change CI worker selection.

```json
{
  "version": 1,
  "targets": {"app": {"dir": ".", "inputs": ["src", "tests", "scripts"]}},
  "environments": {
    "host": {"executor": "native", "identity": "my-pinned-worker", "env": {"MODE": "test"}}
  },
  "checks": {
    "tests": {"kind": "command", "target": "app", "environment": "host", "command": ["bash", "scripts/test.sh"], "timeout": "5m", "artifacts": ["reports/tests.xml"]}
  },
  "runs": {"branch": {"checks": ["tests"]}}
}
```

Commands are argument arrays; shell syntax requires an explicit shell command. Artifact paths are repository-relative regular files and are required on success. Timeouts default to five minutes and terminate the process group, including child processes. Native output is retained in the result; a nonzero exit fails verification, while missing tools, missing artifacts, and timeouts are errors.

The process inherits only `PATH`, `HOME`, and temporary-directory/system-root variables. `LANG` has a stable default. Add fixed nonsecret values through environment/check `env`, or explicit inherited names through environment `pass_env`. Do not put credentials in configuration. The runner supplies `LEVENSHTEIN_SOURCE`, `LEVENSHTEIN_WORKSPACE`, and `LEVENSHTEIN_FRESH` to scripts. An environment can declare `tools`: each entry has a version-printing `command` array and exact expected stdout in `version`. These validations run before preparation/check execution.

A check may reference an entry in top-level `preparations` by ID. Each preparation declares `command`, repository-relative `inputs` and `outputs`, optional `env`, and `timeout`. Preparation runs in the target's workspace directory. Declared output paths and their ancestors must not be symlinks. Checks share compatible successful preparation within a run, with conflicting mutations serialized; missing outputs require preparation again. Preparation records can persist across processes through the local cache. Build/tool caches managed by the repository's commands remain usable.

## Local caching

Successful Dagger checks reuse completed results by default. A native command opts in with `"cache": true`, an environment `identity` identifying a provisioned toolchain/system setup, and an explicit `fresh_command` argument array. The fresh command must actually execute verification, bypassing any test runner verdict cache (for example `go test -count=1`). Every native check selected by a fresh run must declare `fresh_command`, including checks with result caching disabled. It may equal `command` for tools that already execute their tests on each invocation. `LEVENSHTEIN_FRESH` also tells scripts the run policy.

Declare every relevant source, test, script, configuration, local dependency, and lockfile in target inputs. Command options, declared environment values, platform, selected preparation/build definitions, and effective shared implementation also identify results. Declared outputs are excluded from source fingerprints. Undeclared external state cannot be cached safely: leave `cache` off for live-service checks. A native worker's `identity` is a provisioning contract; an eligible cached result can satisfy it without starting tools on the current host. Use fresh audits to obtain new observations.

By default, cache records live under the user's cache directory in `levenshtein/verification-v1`. Override with `--cache-dir /absolute/path/outside/source`. This is private local storage; do not share writable caches between trusted jobs and untrusted PRs. Cross-worker cache transport and CI trust partitioning remain a later implementation step.

Result entries contain original verification time, execution duration, native diagnostics, and required regular-file artifacts. Artifact restoration is atomic and rejects symlinked destinations. Missing/corrupt entries become misses; only successful, complete results are published atomically. If inputs change during execution the result is not cached. Source symlinks disable completed-result reuse until their input scope can be modeled safely.

Preparation records persist across CLI processes only with an explicit provisioned environment `identity`; otherwise stages execute every time, retaining any incremental reuse provided by their native tools. They validate both their inputs and output contents before reuse. A `builds` map uses the same schema as `preparations`; a check can name a `build` stage, which runs after preparation. The build key also includes the selected dependency preparation. Changed keys rerun the native tool in the existing output directory so compatible incremental compilation remains available. Native tools own compatibility checking of their build/download caches. Preparation and build commands always receive `LEVENSHTEIN_FRESH=false`; the final verification command receives the actual policy.

Native execution and artifact restoration serialize per source workspace across local processes; identical result work also coalesces through file locks. Fresh runs bypass completed results while retaining compatible stage outputs. A failed or interrupted execution invalidates the previous result and requires fresh verification on the next attempt, including bypassing underlying Dagger/native verdict caches. Reports expose result cache status/key/lookup time, original verification time/execution duration, total duration, and preparation/build reuse and timings. The source launcher still performs a cached Go build; use a prebuilt CLI when measuring verification startup separately.
