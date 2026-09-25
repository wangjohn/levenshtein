# Standalone runner configuration

`./verify [RUN] --source /path/to/repo` reads `levenshtein.json` from the source repository and runs the checks the named run selects. This page is the guide: what targets, environments, checks, and runs are, worked examples, and how results, the baseline, and caching behave. The [configuration reference](reference/config.md) lists every field with its type and default, and the [CLI reference](reference/cli.md) covers the flags, including `--dry-run`, which prints the plan without starting any executor, and `--jobs`.

Relative target and input paths are resolved from the source repository. Inputs are literal files/directories, not glob patterns. A target's `workspace` can differ from its working directory `dir` and defaults to `.`, the source root; native `command` checks receive it as `LEVENSHTEIN_WORKSPACE`, and preparation stages run in it. Target/workspace directories must exist inside the source repository.

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

[Check kinds](check-kinds.md) lists every `kind`, the executors that can run it, whether its result is cached, and whether it needs a repository-root target; the [check kinds guide](check-kinds-guide.md) says where each belongs and how it behaves. A check carries only the options object of its kind, such as `go-lint`'s optional [`lint` object](#lint-selection) or `go-imports`'s required [`imports` object](#import-rules); the [reference](reference/config.md#checks) lists them all. A kind that needs the repository root takes a target whose `dir` is `"."`; give it the `inputs` and `exclude` that name what it should read, including its root configuration file, such as `.shellcheckrc`, `.gitleaks.toml`, or `osv-scanner.toml`. Every tool version stays pinned in the shared checkout. Local caching is described below.

Without a configuration file, `branch` and `pre-merge` run `go-lint`, `go-vet`, and `go-mod`; `main` also runs `go-vuln`, with `rerun_checks: true`. Every shared kind also gets a run of its own name, such as `./verify go-http` or `./verify deps-vuln`, which is not part of those gates. `go-imports` has no such run, because it has nothing to check without a repository's rules, and neither do `go-mutation`, `semantic-lint`, `command`, and Levenshtein's own `self-test`. An explicit configuration replaces these defaults.

### Runs

A run selects check IDs, optionally per target as described in [one check, several targets](#one-check-several-targets). `rerun_checks: true` forces verification execution while retaining compatible dependency/build caches; scripts see the policy as `LEVENSHTEIN_RERUN_CHECKS`. Any run name can use it; versioned configuration gives `main` no special behavior. Unknown checks, executors, references, and configuration fields fail explicitly.

Recommended run policy:

| Run | Recommended scope | Cache policy |
| --- | --- | --- |
| `branch` | Fast lint and core/focused tests | Aggressive reuse of setup, artifacts, and eligible results |
| `pre-merge` | Critical regression and policy checks for the final candidate | The same aggressive reuse, keyed to the actual selected inputs and scope |
| `main` | Complete applicable suite, scheduled daily by the consumer's CI | Fresh verification (configure `rerun_checks: true`) with dependency/build reuse |
| Custom | Consumer-defined check selection | Explicit choice of normal reuse or fresh verification |

The [check kinds guide](check-kinds-guide.md#named-checks-and-suggested-runs) suggests which run each kind belongs in.

### Lint selection

A `go-lint` check may carry a `lint` object whose `checks` list is appended to the shipped selection in `runner/toolchain.json`:

```json
"cleanup": {"kind": "go-lint", "targets": ["api", "worker"], "environment": "go", "lint": {"checks": ["gocognit", "-unparam"]}}
```

Entries use Staticcheck's `-checks` syntax and the last pattern that matches a rule wins, so `gocognit` turns on a rule the default leaves off and `-unparam` turns off one it runs. A pattern containing `_`, such as `errs_nopanic`, selects [community rules](#community-rule-modules) instead. The object works the same on either executor; `go-http` and `go-sql` keep their fixed rule and take no `lint` object. A pattern that matches no rule the pinned linter registers makes the check an error when it runs, so a misspelled opt-in cannot pass silently. The added patterns are part of the check's result key, so changing them re-runs it. The [reference](reference/config.md#lint) has the pattern syntax, and [changing the selection](rules.md#changing-the-selection-for-one-repository) says when to prefer `//lint:ignore`.

`lint` is an additive, optional field of version 1: a file without it means what it did before and keeps its cached results, so `version` stays `1`. A Levenshtein release older than the field rejects a file that uses it as an unknown field.

### Community rule modules

A top-level `rule_modules` object pins lint rules published as Go modules, which run beside the shipped rules in every `go-lint` check. The key is the module path:

```json
"rule_modules": {
  "github.com/acme/lvrules-errors": {
    "version": "v1.4.0",
    "namespace": "errs",
    "select": ["errs_*", "-errs_wrapf"],
    "advisory": ["errs_sentinel"],
    "settings": {"errs_nopanic": {"allow": "Must,Should"}}
  }
}
```

The pin is exact, and the namespace repeats the module's own, so a rule reports as `errs_nopanic`. `select` turns rules on for every `go-lint` check; a check's own `lint.checks` can add or remove community rules after it, as in `"lint": {"checks": ["errs_wrapf"]}`, and `"lint": {"rule_modules": false}` keeps one check out entirely. `advisory` rules report without failing the check, and `settings` sets an analyzer's flags by rule code; the [reference](reference/config.md#rule_modules) lists every field.

Community rules run only on the Dagger executor: a native `go-lint` check runs the shipped rules and adds a `rule-modules-skipped` warning to its result. The planned modules are part of each check's result key, so moving a pin re-runs the check. A community rule runs code that can read your source; read the [security note](community-rules.md#security) before enabling one on a private repository. [Community lint rules](community-rules.md) has the full contract, pattern rules, warnings, and errors.

`rule_modules` is an additive, optional field of version 1, like `lint`.

### Import rules

A `go-imports` check requires an `imports` object with a nonempty `rules` list. Each rule names the packages it governs, what they may not import (`deny`) or the only things they may import (`allow`), and a `reason` repeated in every finding:

```json
"layers": {"kind": "go-imports", "target": "app", "environment": "go", "imports": {"rules": [
  {"packages": ["./internal/store/..."], "deny": ["./internal/api/...", "net/http"], "tests": "exclude", "reason": "storage never reaches into transport"}
]}}
```

Malformed rules are configuration errors when the run is planned; a `packages` pattern that matches no package is an error when the check runs. The object works the same on either executor, and it is part of the check's result key, so changing a rule re-runs the check. The [reference](reference/config.md#imports) lists the fields and pattern forms, and [import boundaries](check-kinds-guide.md#import-boundaries) says what counts as an import and how findings are reported. Like `lint`, `imports` is an additive field of version 1.

## Source boundaries

For Dagger checks, target `inputs` controls both the files imported into Dagger and source fingerprinting. Declare real, literal repository-relative files/directories, including local dependencies and workspace files outside the target directory. Optional missing paths are allowed; adding them later invalidates the cache. Glob and negation syntax is rejected.

Only declared paths are imported. Within them, `.git`, `.env`, and `.env.*` are excluded, except public `.env.example` templates, and so are the target's `exclude` paths and, under `git` [discovery](#input-discovery), the paths the repository's `.gitignore` files ignore. Symlinks inside declared inputs or along their ancestors are rejected, including aliases to other directories inside the repo. Declare the real paths instead. The no-configuration defaults use `inputs: ["."]`, which imports the whole source tree subject to the exclusions above; use version 1 with explicit product paths for mixed product/private repos.

Native inputs only define cache identity. Native commands are trusted host processes with normal filesystem access; they are not sandboxed by the input list. Native relative symlinks may stay inside the repository, but disable completed-result reuse. Dagger's stricter rule prevents importing undeclared source through aliases.

### Input discovery

A target's `discovery` says how the files under its `inputs` are enumerated. The one enumeration decides everything a check reads: the files its cache key hashes, the files the Dagger path imports, and the files `secrets`, `shell-lint`, and `deps-vuln` scan natively. The key never leaves out a file the check reads.

| Mode | Enumerates |
| --- | --- |
| `git` (default) | The work tree's own files: everything `git ls-files --cached --others --exclude-per-directory=.gitignore` reports under the source, which is tracked files plus untracked files the repository's `.gitignore` files do not ignore |
| `filesystem` | Every path under each declared input, as a directory walk finds it |

Git discovery keeps build output, dependency directories, and editor scratch files out of the fingerprint without naming them, so a target can keep `inputs: ["."]` and still fingerprint only the repository. It runs `git` once per run, found through the absolute entries of `PATH` only (a relative entry would resolve inside the repository being verified), with the source as its working directory and nothing but `PATH` and `HOME` in its environment. The listing is taken again after a check executes, so files a check creates are part of the key its result is cached under. A source that is not a work tree, or a `git` that fails, falls back to the filesystem walk; a failure is reported in the result's cache `Reason`.

**Only the repository's committed `.gitignore` files apply.** A personal `core.excludesFile`, the default `~/.config/git/ignore`, and `.git/info/exclude` are all ignored, so every developer and CI worker enumerates the same files for the same tree.

Some consequences are worth stating. A file the repository's `.gitignore` files ignore is not part of the fingerprint and is not read either: the Dagger import excludes every ignored path under the inputs, and the native scanners read only the listed files. A target whose real inputs are **generated and gitignored** must set `"discovery": "filesystem"`.

**The Go kinds add what the Go toolchain can load.** `go-lint`, `go-vet`, `go-mod`, `go-test`, `go-http`, `go-sql`, `go-vuln`, `go-imports`, `go-generate`, `go-apidiff`, and `go-mutation` read gitignored generated code such as a `*.pb.go` or a generated SDK whatever git lists, so under git discovery their key, their Dagger import, and the native `go-generate` copy also cover the ignored paths the toolchain can load:

- ignored Go, cgo, assembly, and `.syso` sources, and `go.mod`, `go.sum`, `go.work`, and `go.work.sum`;
- any ignored file in a directory that holds, or sits below one that holds, a `.go` file with a `//go:embed` directive;
- anything under a `testdata` directory, which tests read by convention.

An ignored directory with no `.go` file anywhere below it and no embedding package above it, such as `node_modules`, a `_build` directory, or other build output, is left out without being read, so it costs one scan of its names per run. `cmd/go` itself gives no ignored directory a pass by name: `./...` skips `testdata` and directories starting with `.` or `_`, but an explicit import or an embed can still reach them, so the rule goes by content instead. Symlinks in the ignored content that is kept are hashed by their link text, never followed, and do not disable result reuse. The residual risk is a test that opens an ignored file outside `testdata` by a path of its own making, or cgo including an ignored header from another directory; declare such a target `"discovery": "filesystem"`. On a synthetic tree with 30,000 ignored files and 300 `.bin` symlinks in `node_modules`, a warm Go-kind fingerprint takes about 13 ms, against 12 ms for plain git discovery.

A declared input or exclude must be spelled exactly as the repository spells it. On a case-insensitive filesystem `Src` opens `src/`, but git and the import and exclude rules compare exact names, so a path that resolves only under another spelling is a configuration error when the run is planned. An input reached through a symlinked directory is refused like a symlink inside an input: the check runs without result reuse, and the Dagger path rejects it.

In git mode directories have no entries of their own and a tracked path that is absent from disk is skipped rather than recorded as `missing`, so a working tree and a fresh clone of the same content fingerprint identically. The exception is a submodule or an untracked nested repository: git lists it as a single path, so its directory, or a declared input inside it, is walked in full like the filesystem mode does. A declared input that exists neither in the work tree's list nor on disk is still recorded as `missing`, exactly as the filesystem walk records it.

### Excluding paths

A target's `exclude` lists literal repository-relative paths to drop from an otherwise broad input. Excluded paths leave the fingerprint, and for Dagger checks they are also excluded from the imported source, so a declared directory can be imported without its dependency or build output:

```json
{
  "targets": {
    "web": {"dir": "services/web", "inputs": ["services/web"], "exclude": ["services/web/node_modules", "services/web/build"], "discovery": "filesystem"}
  }
}
```

Exclude entries are validated like inputs: clean, relative, and free of pattern or negation characters. `.` is rejected. Symlinks inside an excluded path are not inspected, so excluding a dependency directory also excludes it from the Dagger alias check.

## Results

The CLI emits a versioned JSON report with the resolved plan and a result for every selected check. Results distinguish `passed`, `failed`, `error`, `cancelled`, and `incomplete`, with timing and native output/details. A result may also carry `warnings`, problems that did not change its verdict, each with a `kind` and a `message`; they are kept with a cached result. Each lint finding carries `advisory`, and a `url` for its rule's documentation where there is one; a community finding also names its `source` module. A `go-lint` check can pass with advisory findings in its details. A run succeeds only when every selected check passes; the [exit codes](reference/cli.md#exit-codes) say how the process reports it.

Application repos retain their own CI triggers, workers, schedules, and merge gates. Pin the shared checkout as described in [consumer CI](consumer-ci.md).

### Output formats

`--format` chooses how the report is written. Every format describes the same report, and the exit status is the same whichever one is chosen.

| Format | Writes |
| --- | --- |
| `json` (default) | The full report described above |
| `text` | One line per finding, `file:line:col: CODE message`, then one status line per check and a total |
| `github` | GitHub Actions workflow commands: an `::error` annotation per failing finding, a `::warning` per advisory finding, and one per check that did not pass without a finding to show |
| `sarif` | SARIF 2.1.0 for GitHub code scanning (`github/codeql-action/upload-sarif`) |

**Text.** Findings are grouped by check in plan order and sorted by file, line, column, and code within a check. Paths are relative to the source root, as they are in the JSON report. A check that wraps a whole tool, such as `go-vet`, `go-mod`, or `workflow-lint`, reports one finding per module whose message is the tool's own output; text prints its directory and code on one line and the output indented below it. A hint, when there is one, follows on an indented `hint:` line. An [advisory finding](community-rules.md) is marked `[advisory]` after its code. After the findings, each check gets one line with its status and a note: how many failing findings it has, how many the baseline accepted, how many are advisory, the first line of an error, or `cached`. An error of more than one line is repeated in full below the table. Findings the [baseline](#baseline) accepts are counted, not listed.

**GitHub.** A finding from `go-lint`, `go-http`, `go-sql`, `go-mutation`, `go-imports`, `go-generate`, `go-apidiff`, `shell-lint`, `secrets`, or `deps-vuln`, and a `baseline-stale` finding, is annotated at its file, line, and column; a module-level finding is annotated without a file. A check that ended in `error`, `incomplete`, or `cancelled`, or that failed without any finding to show (a `command` check, for example), gets one annotation with its error. Every value is escaped as GitHub's runner expects (`%`, carriage return, and newline in messages; also `:` and `,` in properties), so a message or file name cannot end an annotation or start another command. An advisory finding is a `::warning` rather than an `::error`, and its rule's page, when it has one, follows the message. Baselined findings are not annotated.

**SARIF.** The file has one run, whose tool is `Levenshtein`, with one rule per finding code, sorted by code. Code scanning treats a run as one tool's analysis and refuses several runs of the same tool in one upload, so the checks share the run; each result names its check and kind under `properties`. A finding that fails its check has level `error`; an advisory or baselined one has level `warning`. A rule links to its documentation where there is a stable page (Levenshtein's `LV` rules, Staticcheck's codes, and a community rule's own `url`) and carries the hint as its help text. Only findings with a source location become results, because code scanning needs a file and line for each one: module-level findings, and checks that did not reach a verdict, are listed as tool execution notifications instead, and `executionSuccessful` is false when any check did not reach a verdict. Locations are relative to `%SRCROOT%`, the checkout `upload-sarif` resolves them against. When a baseline was applied, new findings have `"baselineState": "new"`, and baselined ones `"unchanged"`. `semantic-lint`'s advisory findings stay in the JSON report only, in every format.

`--path-prefix` places every path in text, GitHub, and SARIF output under a subdirectory of the checkout, and `--render` writes a saved JSON report in another format without running anything, so a CI job can keep the JSON report and derive the others from it. The [CLI reference](reference/cli.md#rendering-a-saved-report) has both.

### Fix hints

A finding whose code has a mechanical, well-known fix carries a one-line `hint` in the JSON report, and text output prints it below the finding. The table lives in `internal/verify/hints.go` and is deliberately small:

| Code | Hint |
| --- | --- |
| `LV1001`–`LV1006` | What the rule wants, with a link to its section of [the rules](rules.md) |
| `LV1005` | `gofmt -w <file>` |
| `minmax`, `mapsloop`, `slicescontains`, `stringscutprefix`, `stringsseq` | `go fix -<rule> ./...` in the module |
| `errcheck` | Handle the error, or discard it explicitly with `_ =` and a comment giving the reason |
| `go-mod` | `go mod tidy` in the module, when tidy's diff is the finding; a download that fails `go mod verify` gets no hint |
| `go-generate` | `go generate ./...` in the module, then commit what it writes |
| `baseline-stale` | Delete the entry or lower its count, or rewrite the file with `--write-baseline` |

Levenshtein never applies a hint or changes a file. Hints are added to the finished report on the CLI side, never by an executor, so they are not part of any cached result, and `--render` fills in hints a saved report lacks.

## Baseline

A baseline lets a repository adopt the full rule set while it still has findings. The findings that exist today are recorded in a checked-in file and are reported without failing the run; any new finding fails as usual; and an entry for a finding that no longer occurs fails the run until it is deleted, so the file only shrinks as the code is fixed. Without a `baseline` setting nothing changes.

```json
{
  "version": 1,
  "targets": {"app": {"dir": ".", "inputs": ["."]}},
  "environments": {"host": {"executor": "native"}},
  "checks": {"lint": {"kind": "go-lint", "target": "app", "environment": "host"}},
  "runs": {"branch": {"checks": ["lint"]}, "main": {"checks": ["lint"], "rerun_checks": true}},
  "baseline": ".levenshtein/baseline.json"
}
```

`baseline` is a top-level, repository-relative path, validated like other paths: clean, relative, and not a private path such as `.env`. It is one file for the whole repository rather than an option on each check because every entry already says which kind and target directory it belongs to, one command writes it, and a Dagger check and a native check of the same kind over the same target share entries. A missing file records nothing. Like `lint`, it is an additive, optional field of version 1.

### Commands

```sh
./levenshtein/verify main --source ./app --write-baseline   # record every current finding
./levenshtein/verify branch --source ./app                  # new findings fail; baselined ones do not
./levenshtein/verify main --source ./app --no-baseline      # report every finding, ignoring the file
```

`--write-baseline` runs the run without applying the baseline, prints the report in the chosen format, and then rewrites the file from it. It refuses, exits 1, and leaves the file untouched when any check ended in `error`, `incomplete`, or `cancelled`, because what that check would have found is unknown. Advisory findings, which never fail a check, are never recorded. Otherwise it exits 0 once the file is written, whatever the run found, and reports on standard error how many entries it wrote and how many findings it added and removed; failing checks of a kind a baseline cannot hold are named there too. Entries for a kind and target directory the run did not cover are kept as they were, so recording `branch` does not drop entries only `main` covers; so are `go-lint` entries for a rule no check of the run ran (see below). Use a run that covers the whole repository to write the first file. `--write-baseline` needs `baseline` in the configuration, and exits 2 without it.

`--no-baseline` reports every finding as if no baseline were configured, and does not read the file. Use it to see the whole debt.

### The file

```json
{
  "version": 1,
  "findings": [
    {"kind":"go-lint","dir":".","file":"internal/store/store.go","code":"errcheck","message":"unchecked error","count":2},
    {"kind":"go-lint","dir":".","file":"internal/store/store.go","code":"SA5001","message":"should check error returned from os.Open() before deferring f.Close()","count":1}
  ]
}
```

Each entry records how many findings share a check kind, a target directory (the target's `dir`), a repository-relative file, a code, and a message. `--write-baseline` writes one entry per line, sorted by those fields, so a change reads as added and removed lines. Every field is required, `count` is at least 1, and unknown fields, repeated entries, and any version other than 1 are rejected. An entry whose kind cannot be baselined is a configuration error (exit 2), as is any other malformed file; both are reported before any check runs.

### Matching

A finding matches an entry when the check's kind and target directory, the finding's file and code, and its normalized message are all equal. Normalizing collapses runs of whitespace and removes Go source positions such as `config.go:12:3` from the message, so a message that mentions another line still matches after that line moves. Line and column numbers are never compared, so edits elsewhere in a file, including ones that move the finding, do not break an entry. `count` findings match an entry; one more with the same key is new.

The limits follow from that key. Moving code to another file, renaming the file, or changing the target's `dir` makes its findings new and their old entries stale. Two findings that differ only in line number are indistinguishable, so fixing one and introducing another like it in the same file keeps the count and passes. A rule whose message names a value, such as an identifier, makes a renamed identifier a new finding.

### Outcomes

The baseline applies to checks of a supported kind that reached a verdict (`passed` or `failed`):

| Condition | Outcome |
| --- | --- |
| A finding matches an entry with count left | Reported with `"baselined": true`; it does not fail the check |
| A finding matches no entry, or its entry's count is used up | Fails the check, as without a baseline |
| An entry records more findings than a check that covers it found | A `baseline-stale` finding at the entry's line in the baseline file, which fails the check |
| Every finding is baselined and no entry is stale | The check passes and its error is cleared |

An entry is judged only by a check of its kind whose target directory is the entry's `dir` and which reached a verdict in this run, so a run that does not cover a module never reports that module's entries as stale, and a check that ended in `error` judges nothing. A `go-lint` check also judges only the entries of rules it ran: a native check, which skips community rules, leaves community-rule entries alone, and so does a check whose `lint.checks` turns a rule off (such as `-unparam`) for that rule's entries. When several such checks run, for example a native and a Dagger `go-lint` over the same target, each matches the entry on its own, and it is stale only when every one of them left part of it unused; a stale finding is attached to the first. The report's top-level `baseline` object gives the file and the number of baselined findings and stale entries. Text and GitHub output leave baselined findings out and count them; SARIF keeps them as `unchanged`.

### Supported kinds

| Kind | Baseline |
| --- | --- |
| `go-lint`, `go-http`, `go-sql`, `go-imports`, `shell-lint` | Supported: each finding names one source location and a message that does not repeat it. A `go-imports` baseline lets a repository adopt a layering rule its existing code breaks, and a `shell-lint` baseline lets it adopt ShellCheck over scripts it already has, each failing only on new findings |
| `secrets`, `deps-vuln` | Not supported: a committed credential or a known-vulnerable dependency should not be deferred. Allowlist a false positive in `.gitleaksignore` or `osv-scanner.toml` instead, where the reason sits beside it |
| `go-generate`, `go-apidiff` | Not supported: stale generated code is fixed by regenerating it, and a breaking API change is judged against the branch's merge base, so neither is a debt a repository carries between changes |
| `go-vet`, `workflow-lint`, `workflow-security`, `go-mod`, `go-vuln`, `go-test` | Not supported: each reports one finding per module carrying the tool's whole output, line numbers included, which no entry could match across unrelated edits; security and test failures should not be deferred either |
| `go-mutation` | Not supported: it has its own [accepted-survivors file](mutation.md#accepted-survivors) |
| `command`, `semantic-lint`, `self-test` | Not supported: they report no located findings |

### Caching

The baseline is applied on the CLI side, to the finished report, after each result was produced or restored from the cache, and never inside a cached result. The cache stores only passing results, and a check whose findings are all baselined still fails underneath, so it is never cached: every run executes it again, and because its last execution did not publish a successful result, it executes fresh, with a throwaway Staticcheck analysis cache (see [local caching](#local-caching)). The verdict is the same on every run for the same source. The cost is the check's full analysis time on each run until its baselined findings are fixed and it passes; a check without findings is cached as before.

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

A `command` check's `command` object holds the argument array to run and its options: a fresh-run variant, extra variables, a timeout, stages to run first, artifacts it must produce, and whether its result may be cached. The [reference](reference/config.md#command) lists each field and its default. Arguments are not parsed by a shell; shell syntax requires an explicit shell command. The timeout stops the whole process group, including child processes. Native output is retained in the result; a nonzero exit fails verification, while missing tools, missing artifacts, and timeouts are errors.

The process inherits only `PATH`, `HOME`, and temporary-directory/system-root variables. `LANG` has a stable default. Add fixed nonsecret values through the environment `env` or, for command checks, `command.env`; pass explicit inherited names through environment `pass_env`. Do not put credentials in configuration. The runner supplies `LEVENSHTEIN_SOURCE`, `LEVENSHTEIN_WORKSPACE`, and `LEVENSHTEIN_RERUN_CHECKS` to scripts. Native scripts must honor `LEVENSHTEIN_RERUN_CHECKS=true` by bypassing cached verification results while retaining compatible dependency/build caches. An environment can declare `tools`, each a version-printing command and its exact expected output, which are checked before preparation or check execution.

A command check may name an entry in top-level `preparations` in `command.preparation`, and one in `builds` in `command.build`. A stage declares its command, the repository-relative `inputs` that key it, and the `outputs` it produces ([reference](reference/config.md#preparations-and-builds)). It runs in the target's workspace directory. Declared output paths and their ancestors must not be symlinks. When the environment declares an `identity` and a result cache is configured, checks share compatible successful preparation, within a run and across processes, with conflicting mutations serialized; missing outputs require preparation again. Without both, every check runs its own stages. Build/tool caches managed by the repository's commands remain usable.

## Native Go checks

Most shared kinds also run on a `native` environment, on the host rather than in a container; the Executors column of [check kinds](check-kinds.md) says which. Bind the check to a native environment; nothing else about the check changes:

```json
{
  "version": 1,
  "targets": {"api": {"dir": "services/api", "inputs": ["services/api", "go.work"]}},
  "environments": {
    "host": {"executor": "native"},
    "go": {"executor": "dagger"}
  },
  "checks": {
    "lint": {"kind": "go-lint", "target": "api", "environment": "host"},
    "vet": {"kind": "go-vet", "target": "api", "environment": "host"},
    "vulnerabilities": {"kind": "go-vuln", "target": "api", "environment": "go"}
  },
  "runs": {"branch": {"checks": ["lint", "vet"]}}
}
```

A kind's options object works the same way on either executor, except that a native `go-lint` check skips [community rules](#community-rule-modules) with a warning; a `command` or `semantic` object is rejected. The environment may still declare `identity`, `env`, `pass_env`, and pinned `tools`. Dagger stays the default for a repository with no configuration file.

The host must supply the Go in the shared checkout's `.go-version`; toolchain switching is disabled, so a different host Go is used as-is rather than silently replaced. Levenshtein builds the helper tools (`levenshtein-lint`, `levenshtein-gocheck`, `actionlint`, `govulncheck`, `apidiff`) from the pinned shared checkout into `<cache-dir>/tools/` on first use, serialized by a file lock, and relies on Go's own build cache for repeat builds. `workflow-security` instead downloads the pinned zizmor release archive for the host into `<cache-dir>/tools/zizmor-<version>/` and verifies its SHA-256 before extracting it, on every run; macOS and Linux on amd64 and arm64 are pinned. `shell-lint` and `deps-vuln` do the same with ShellCheck and osv-scanner, into `<cache-dir>/tools/shellcheck-<version>/` and `<cache-dir>/tools/osvScanner-<version>/`, and `secrets` builds gitleaks from `runner/tools/gitleaks` like the other helpers. `secrets` and `deps-vuln` copy the target's declared, unexcluded files into a temporary directory and scan that, so they read what the Dagger path imports and nothing else; `deps-vuln` also needs to reach `api.osv.dev`, so a host behind a proxy passes its settings with `pass_env`. `go-test` builds no helper but runs `go test -race`, which needs cgo: it sets `CGO_ENABLED=1` and is an error when the C compiler `go env CC` names is not on the check's `PATH`. Staticcheck's analysis cache lives in `<cache-dir>/staticcheck`; a fresh run points it at a throwaway directory instead. An analyzer that fails stops the run before its package is cached, so a failed run's results are never reused. Without a cache directory, each check builds into a temporary directory it removes afterwards.

Workspace selection matches what the container would see. The container imports only declared inputs less the target's `exclude`, so the host runs with `GOWORK` set to the nearest `go.work` between the target directory and the source root that an input covers and no exclude drops, and `GOWORK=off` when there is none; a `go.work` above the source, or one the target does not declare, is never used. Declare `go.work` (and `go.work.sum`) in the target's `inputs` to analyze the module in workspace mode. `go-mod` is the exception on both executors: it always runs with `GOWORK=off`, because `go mod tidy` checks one module's own manifests whatever workspace it belongs to, and `go mod verify` then covers that module's requirements rather than every workspace member's. Give each workspace module its own target.

The trade-off is the point of the choice. The container pins the Go version, the operating system, the C toolchain, and the default build tags, so its verdict is reproducible anywhere. The native executor uses the host's, which is faster and needs no Docker, but means analysis reflects the worker's platform and build tags. It is also not isolated: the host's `go list` and `go vet` may download modules and invoke cgo toolchains against the consumer's code outside any container, as trusted native commands do. Results are fingerprinted accordingly: a native shared Go check's cache key includes the host's `go env GOVERSION GOOS GOARCH`, the Go settings that change a build (`GOFLAGS`, `GOEXPERIMENT`, `CGO_ENABLED`, `CC` and the other cgo settings, and the architecture levels), including ones set with `go env -w`, and the shared checkout's `runner/` directory (the linter, its rule list, and the pinned tools), so a worker on a different Go, or a pin that changes the rules, never reuses another's result. Reports have the same shape either way, with the same rule codes, messages, and repository-relative locations.

Result caching follows the kind rather than a `command.cache` flag these checks do not have: a native result is reused exactly as a Dagger one is, and a kind the Results cached column of [check kinds](check-kinds.md) marks as never reused is not reused on either executor. `go-generate` runs its generators in a temporary copy of the target's declared inputs, never in the working tree ([generated code](check-kinds-guide.md#generated-code)).

## Semantic lint

`semantic-lint` is a native check with no command. It diffs the working tree against a base branch, asks a pinned Jev model the shared question catalog, and records advisory findings. Its optional `semantic` object sets the base branch, the model, the timeout, and the request budgets ([reference](reference/config.md#semantic)); it reads `TYPESAFE_API_KEY` from the host environment without `pass_env`, and it is never cached. See [semantic lint](semantic-lint.md) for the questions, key handling, and limits.

## Mutation testing

`go-mutation` is a Dagger check that mutates the Go files a branch changed and fails when a test covers a mutant but does not catch it. The CLI chooses the files on the host, because the Dagger source has no `.git`. Its optional `mutation` object sets the base branch, the scope, the accepted-survivors file, build tags, and the timeout ([reference](reference/config.md#mutation)), and its result is never reused by the CLI cache. See [mutation testing](mutation.md) for the outcomes and the accepted-survivors file.

## Local caching

Successful Dagger checks, and native shared Go checks, reuse completed results by default, except `go-vuln`, `deps-vuln`, and `go-mod`: vulnerability scans always execute against current advisory data, and `go mod verify` always checks the module cache as it is now, which no input fingerprint covers, even with `cache: true` or in custom runs. `go-test` results are reused, so its target's `inputs` must cover everything the tests read; tests that depend on anything else belong in a `command` check (see [tests](check-kinds-guide.md#tests)). `go-mutation` and `go-apidiff` results are not reused either, because the files one mutates and the API the other compares with depend on the base branch; Dagger still returns an identical call from its own cache. A native command opts in with `command.cache: true`, an environment `identity` identifying a provisioned toolchain/system setup, and an explicit `command.rerun_args` argument array. The fresh command must actually execute verification, bypassing any test runner verdict cache (for example `go test -count=1`). Every native `command` check selected by a fresh run must declare `rerun_args`, including checks with result caching disabled; `semantic-lint` always executes and needs none. It may equal `args` for tools that already execute their tests on each invocation. `LEVENSHTEIN_RERUN_CHECKS` also tells scripts the run policy.

Declare every relevant source, test, script, configuration, local dependency, and lockfile in target inputs. Command options, declared environment values, platform, selected preparation/build definitions, target `discovery`/`exclude`, and effective shared implementation also identify results. The pinned shared checkout is snapshotted once per CLI process; edit it between runs, not during one. Declared outputs are excluded from source fingerprints. Undeclared external state cannot be cached safely: leave `cache` off for live-service checks. A native worker's `identity` is a provisioning contract; an eligible cached result can satisfy it without starting tools on the current host. Use fresh audits to obtain new observations.

Fingerprinting hashes file content, so it is memoized. Within a process a file whose size, modification time, inode, and permissions are all unchanged reuses its recorded hash instead of being read again, and every target in the run shares those records. The memo is persisted under `<cache-dir>/stat/` at the end of a run and reloaded at the start of the next one. A persisted record is only a hint: every lookup revalidates the file's current stat, a corrupt or unreadable record is ignored rather than fatal, and an entry whose modification time is within two seconds of the moment its content was read is never trusted from disk, because the filesystem may not have been able to distinguish a later write in the same timestamp tick. At the end of a run, entries for files that were deleted or changed since they were recorded are dropped from the record. Within one process, an edit that preserves a file's size, permissions, inode, **and** modification time is not detected; on a filesystem with nanosecond timestamps two different writes cannot share a modification time. Content hashing runs concurrently, bounded by `GOMAXPROCS`.

By default, cache records live under the user's cache directory in `levenshtein/verification-v1`. Override with `--cache-dir`, which must be outside both the source and the shared checkout. This is private local storage; do not share writable caches between trusted jobs and untrusted PRs. Levenshtein's own `verify.yml` restores that directory across GitHub Actions workers under a per-commit key with a prefix `restore-keys` fallback, and saves it on every event: a pull request's entries stay in that pull request's cache scope, which `main` never reads. See [Levenshtein's own CI](setup.md#result-cache-trust). The GitHub Action does the same for consumers ([caching and trust](consumer-ci.md#github-actions)).

Result entries contain original verification time, execution duration, native diagnostics, and required regular-file artifacts. Artifact restoration is atomic and rejects symlinked destinations. Missing/corrupt entries become misses; only successful, complete results are published atomically. If inputs change during execution the result is not cached. Source symlinks disable completed-result reuse until their input scope can be modeled safely.

Preparation records are kept only with an explicit provisioned environment `identity` and a result cache; otherwise stages execute for every check, retaining any incremental reuse provided by their native tools. They validate both their inputs and output contents before reuse. A `builds` map uses the same schema as `preparations`; a command check can name a `command.build` stage, which runs after preparation. The build key also includes the selected dependency preparation. Changed keys rerun the native tool in the existing output directory so compatible incremental compilation remains available. Native tools own compatibility checking of their build/download caches. Preparation and build commands always receive `LEVENSHTEIN_RERUN_CHECKS=false`; the final verification command receives the actual policy.

Native execution and artifact restoration serialize per source workspace across local processes; identical result work also coalesces through file locks. Fresh runs bypass completed results while retaining compatible stage outputs. A failed or interrupted execution invalidates the previous result and requires fresh verification on the next attempt, including bypassing underlying Dagger/native verdict caches. Reports expose result cache status/key/lookup time, original verification time/execution duration, total duration, and preparation/build reuse and timings. The source launcher still performs a cached Go build; use a prebuilt CLI when measuring verification startup separately.

Directory paths must be clean and relative to the repository. Native relative symlinks are allowed only when they stay inside the repository; absolute aliases are rejected. Dagger source inputs reject all symlinks as described above.
