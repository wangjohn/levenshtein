# Standalone runner configuration

`./verify [RUN] --source /path/to/repo` builds the small Go CLI through Go's incremental build cache and runs it. The launcher builds with the Go version pinned in `.go-version`, which Go provisions itself when the host version differs; Dagger is needed only when a selected Dagger check executes. A prebuilt CLI can be used directly with `--shared /path/to/pinned/levenshtein`.

`./verify branch --dry-run` reads configuration and returns the selected plan without starting execution tools. Relative target and input paths are resolved from the source repository. Inputs are literal files/directories, not glob patterns. Workspace context can differ from the check's working directory. Target/workspace directories must exist inside the source repository.

## Version 1

```json
{
  "version": 1,
  "targets": {
    "api": {"dir": "services/api", "workspace": ".", "inputs": ["services/api", "go.work", "contracts"]},
    "worker": {"dir": "services/worker", "workspace": ".", "inputs": ["services/worker", "go.work"]}
  },
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "cleanup": {"kind": "go-lint", "targets": ["api", "worker"], "environment": "go"},
    "workflows": {"kind": "workflow-lint", "target": "api", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["cleanup/api"]},
    "pre-merge": {"checks": ["cleanup"]},
    "daily": {"checks": ["cleanup"], "rerun_checks": true}
  }
}
```

### One check, several targets

A check declares either one `target` or a `targets` list, never both and never
neither. `targets` must be nonempty and must not repeat a name.

A check with `targets` plans one check per entry, in the order they are
declared, with the ID `<check>/<target>`: `cleanup` above plans `cleanup/api`
and then `cleanup/worker`. A run selects either the check ID, which takes every
target, or a single `<check>/<target>`, as `branch` does. Referencing
`<check>/<target>` for a check declared with the singular `target` is an error,
as is naming a target the check does not declare, or selecting the same planned
check twice in one run.

Each expanded check keeps its own cache identity, inputs, and result; the
expansion is a way to write one declaration instead of one per module.

Dagger checks include `go-lint`, `go-vet`, `go-http`, `go-sql`, `go-vuln`, `workflow-lint`, and Levenshtein's own `self-test`; native checks use `command` or the advisory [`semantic-lint`](semantic-lint.md). See the [shared checks](checks.md) for scope and examples. Workflow lint requires a repository-root target. Go tool versions remain pinned in the shared checkout. Local caching is described below.

Without a configuration file, `branch` and `pre-merge` run `go-lint` and `go-vet`; `main` also runs `go-vuln`. Named runs for each shared check are available. An explicit configuration replaces these defaults.

A run selects check IDs, optionally per target as described in [one check, several targets](#one-check-several-targets). `rerun_checks: true` forces verification execution while retaining compatible dependency/build caches. It replaces the earlier `fresh` setting; use `rerun_checks` in configuration and `LEVENSHTEIN_RERUN_CHECKS` in scripts. Any run name can use it; versioned configuration gives `main` no special behavior. Unknown checks, executors, references, and configuration fields fail explicitly.

Recommended run policy:

| Run | Recommended scope | Cache policy |
| --- | --- | --- |
| `branch` | Fast lint and core/focused tests | Aggressive reuse of setup, artifacts, and eligible results |
| `pre-merge` | Critical regression and policy checks for the final candidate | The same aggressive reuse, keyed to the actual selected inputs and scope |
| `main` | Complete applicable suite, scheduled daily by the consumer's CI | Fresh verification (configure `rerun_checks: true`) with dependency/build reuse |
| Custom | Consumer-defined check selection | Explicit choice of normal reuse or fresh verification |

## Source boundaries

For Dagger checks, target `inputs` controls both the files imported into Dagger and source fingerprinting. Declare real, literal repository-relative files/directories, including local dependencies and workspace files outside the target directory. Optional missing paths are allowed; adding them later invalidates the cache. Glob and negation syntax is rejected.

Only declared paths are imported. Within them, `.git`, `.env`, and `.env.*` are excluded, except public `.env.example` templates. Symlinks inside declared inputs or along their ancestors are rejected, including aliases to other directories inside the repo. Declare the real paths instead. The no-configuration defaults use `inputs: ["."]`, which imports the whole source tree subject to the exclusions above; use version 1 with explicit product paths for mixed product/private repos.

Native inputs only define cache identity. Native commands are trusted host processes with normal filesystem access; they are not sandboxed by the input list. Native relative symlinks may stay inside the repository, but disable completed-result reuse. Dagger's stricter rule prevents importing undeclared source through aliases.

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
    "tests": {"kind": "command", "target": "app", "environment": "host", "command": {"args": ["bash", "scripts/test.sh"], "timeout": "5m", "artifacts": ["reports/tests.xml"]}}
  },
  "runs": {"branch": {"checks": ["tests"]}}
}
```

A check carries the options of its kind and no others: a `command` check has a `command` object, `semantic-lint` may have a `semantic` object, and a Dagger kind has neither. `command` holds `args`, `rerun_args`, `env`, `timeout`, `preparation`, `build`, `artifacts`, and `cache`. `args` and `rerun_args` are argument arrays; shell syntax requires an explicit shell command. Artifact paths are repository-relative regular files and are required on success. Timeouts default to five minutes and terminate the process group, including child processes. Native output is retained in the result; a nonzero exit fails verification, while missing tools, missing artifacts, and timeouts are errors.

The process inherits only `PATH`, `HOME`, and temporary-directory/system-root variables. `LANG` has a stable default. Add fixed nonsecret values through the environment `env` or, for command checks, `command.env`; pass explicit inherited names through environment `pass_env`. Do not put credentials in configuration. The runner supplies `LEVENSHTEIN_SOURCE`, `LEVENSHTEIN_WORKSPACE`, and `LEVENSHTEIN_RERUN_CHECKS` to scripts. Native scripts must honor `LEVENSHTEIN_RERUN_CHECKS=true` by bypassing cached verification results while retaining compatible dependency/build caches. An environment can declare `tools`: each entry has a version-printing `command` array and exact expected stdout in `version`. These validations run before preparation/check execution.

A command check may reference an entry in top-level `preparations` by its `command.preparation` ID. Each preparation declares `command`, repository-relative `inputs` and `outputs`, optional `env`, and `timeout`. Preparation runs in the target's workspace directory. Declared output paths and their ancestors must not be symlinks. When the environment declares an `identity` and a result cache is configured, checks share compatible successful preparation, within a run and across processes, with conflicting mutations serialized; missing outputs require preparation again. Without both, every check runs its own stages. Build/tool caches managed by the repository's commands remain usable.

## Semantic lint

`semantic-lint` is a native check with no command. It diffs the working tree against a base branch, asks a pinned Jev model the shared question catalog, and records advisory findings. Its optional `semantic` object accepts `base`, `model`, and `timeout`; it reads `TYPESAFE_API_KEY` from the host environment without `pass_env`, and it is never cached. See [semantic lint](semantic-lint.md) for the questions, key handling, and limits.

## Local caching

Successful Dagger checks reuse completed results by default, except `go-vuln`: vulnerability scans always execute against current advisory data, even with `cache: true` or in custom runs. A native command opts in with `command.cache: true`, an environment `identity` identifying a provisioned toolchain/system setup, and an explicit `command.rerun_args` argument array. The fresh command must actually execute verification, bypassing any test runner verdict cache (for example `go test -count=1`). Every native `command` check selected by a fresh run must declare `rerun_args`, including checks with result caching disabled; `semantic-lint` always executes and needs none. It may equal `args` for tools that already execute their tests on each invocation. `LEVENSHTEIN_RERUN_CHECKS` also tells scripts the run policy.

Declare every relevant source, test, script, configuration, local dependency, and lockfile in target inputs. Command options, declared environment values, platform, selected preparation/build definitions, and effective shared implementation also identify results. The pinned shared checkout is snapshotted once per CLI process; edit it between runs, not during one. Declared outputs are excluded from source fingerprints. Undeclared external state cannot be cached safely: leave `cache` off for live-service checks. A native worker's `identity` is a provisioning contract; an eligible cached result can satisfy it without starting tools on the current host. Use fresh audits to obtain new observations.

By default, cache records live under the user's cache directory in `levenshtein/verification-v1`. Override with `--cache-dir /absolute/path/outside/source`. This is private local storage; do not share writable caches between trusted jobs and untrusted PRs. Levenshtein's own `verify.yml` restores that directory across GitHub Actions workers with an exact cache key and saves only from trusted (non-fork) refs; see [Levenshtein's own CI](setup.md#levenshteins-own-ci). Consumer cross-worker templates remain separate.

Result entries contain original verification time, execution duration, native diagnostics, and required regular-file artifacts. Artifact restoration is atomic and rejects symlinked destinations. Missing/corrupt entries become misses; only successful, complete results are published atomically. If inputs change during execution the result is not cached. Source symlinks disable completed-result reuse until their input scope can be modeled safely.

Preparation records are kept only with an explicit provisioned environment `identity` and a result cache; otherwise stages execute for every check, retaining any incremental reuse provided by their native tools. They validate both their inputs and output contents before reuse. A `builds` map uses the same schema as `preparations`; a command check can name a `command.build` stage, which runs after preparation. The build key also includes the selected dependency preparation. Changed keys rerun the native tool in the existing output directory so compatible incremental compilation remains available. Native tools own compatibility checking of their build/download caches. Preparation and build commands always receive `LEVENSHTEIN_RERUN_CHECKS=false`; the final verification command receives the actual policy.

Native execution and artifact restoration serialize per source workspace across local processes; identical result work also coalesces through file locks. Fresh runs bypass completed results while retaining compatible stage outputs. A failed or interrupted execution invalidates the previous result and requires fresh verification on the next attempt, including bypassing underlying Dagger/native verdict caches. Reports expose result cache status/key/lookup time, original verification time/execution duration, total duration, and preparation/build reuse and timings. The source launcher still performs a cached Go build; use a prebuilt CLI when measuring verification startup separately.

Directory paths must be clean and relative to the repository. Native relative symlinks are allowed only when they stay inside the repository; absolute aliases are rejected. Dagger source inputs reject all symlinks as described above.
