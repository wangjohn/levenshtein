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

Currently supported checks are `go-lint` and Levenshtein's own `self-test`, using the Dagger executor. Tool versions remain pinned in the shared checkout. Native commands and result caching are subsequent implementation steps.

A run selects check IDs. `fresh: true` forces verification execution while retaining compatible dependency/build caches. Any run name can use it; versioned configuration gives `main` no special behavior. Unknown checks, executors, references, and configuration fields fail explicitly.

Legacy `modules` and array-valued `runs` remain supported. They translate into Go targets/checks, and legacy `main` retains fresh behavior. A source without configuration still receives the original single-module Go defaults.

## Results

The CLI emits a versioned JSON report with the resolved plan and a result for every selected check. Results distinguish `passed`, `failed`, `error`, `cancelled`, and `incomplete`, with timing and native output/details. A run succeeds only when every selected check passes. Planning/configuration errors exit 2; unsuccessful verification exits 1.

Application repos retain their own CI triggers, workers, schedules, and merge gates. Pin the shared checkout as described in [consumer CI](consumer-ci.md).

Directory paths must be clean and relative to the repository. Relative symlinks are allowed only when they stay inside the repository; absolute symlink aliases are rejected, even when they point back inside it. Use the real directory path or a relative alias.
