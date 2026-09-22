# Shared checks

Every shared check kind runs the same pinned tools locally and in any CI provider. Consumer repos inherit them by updating their pinned Levenshtein revision. No tool installation is needed outside Dagger. [Named checks](#named-checks-and-suggested-runs) lists every kind and where it belongs; the rules below are what `./verify go-lint` enforces.

## Go lint rules

| Rule | Policy |
| --- | --- |
| `SA*` | Correctness checks in the pinned Staticcheck release |
| `S1*` | Simplifications whose result is objectively simpler code |
| `ST1*` | Style rules, minus the six naming and documentation rules listed below |
| `QF1*` | Refactorings Staticcheck ships as quick fixes |
| `U1000` | Unused unexported code |
| `errcheck` | Report implicitly discarded errors; explicit `_ =` remains allowed |
| `exhaustive` | Require enum switches to cover declared values |
| `bodyclose`, `sqlclosecheck`, `rowserrcheck`, `noctx` | Resources a program opens and never closes, and calls that drop the context |
| `nilness`, `unusedwrite`, `errorlint`, `nilerr`, `durationcheck`, `reassign`, `wastedassign` | Behavior that is wrong rather than unidiomatic |
| `musttag` | Tag every exported field of a struct passed to a JSON, XML, YAML, or TOML encoder or decoder, so renaming a Go field cannot silently change the format ([settings](#upstream-analyzer-settings)) |
| `recvcheck` | Give a type all pointer or all value receivers; a mix means a value and a pointer have different method sets, and value methods work on a copy |
| `unparam` | Unexported functions with a parameter no caller needs, a parameter that always receives the same value, or a result no caller uses |
| `intrange`, `usestdlibvars`, `perfsprint`, `predeclared`, `errname` | Modern, consistent standard-library usage |
| `minmax`, `mapsloop`, `slicescontains`, `stringscutprefix`, `stringsseq` | Hand-written loops and comparisons that one standard-library call replaces; `go fix` applies the fix |
| `thelper`, `tparallel`, `testifylint` | Mistakes that only appear in `_test.go` files |
| LV1001 | Give enum-like strings defined types and typed constants |
| LV1002 | Construct new structs together with literals, without opt-in markers |
| LV1003 | Declare each struct field on its own line |
| LV1004 | Separate top-level declarations with a blank line |
| LV1005 | Keep every file formatted the way `gofmt` writes it |
| LV1006 | Give every test a way to fail, and do not skip a test unconditionally |

## The Staticcheck selection

`runner/toolchain.json` selects `all` and then turns off six style rules that impose naming and documentation conventions a shared runner should not decide for its consumers:

| Rule | Why it is off |
| --- | --- |
| [ST1000](https://staticcheck.dev/docs/checks/#ST1000) | Requires a package comment in a fixed form |
| [ST1003](https://staticcheck.dev/docs/checks/#ST1003) | Imposes an initialism list on every identifier |
| [ST1016](https://staticcheck.dev/docs/checks/#ST1016) | Requires one receiver name per type |
| [ST1020](https://staticcheck.dev/docs/checks/#ST1020) | Requires a comment on every exported function |
| [ST1021](https://staticcheck.dev/docs/checks/#ST1021) | Requires a comment on every exported type |
| [ST1022](https://staticcheck.dev/docs/checks/#ST1022) | Requires a comment on every exported variable |

`all` also selects the bare analyzers compiled into the binary, so `errcheck`, `exhaustive`, the resource and correctness analyzers, and the `LV*` rules are part of it. A consumer that wants less can pass its own selection; the same `-checks` syntax applies, and a `-` prefix removes a rule a wider pattern selected.

Upstream analyzers run over generated files, so the facts they export stay correct, but their diagnostics there are dropped: nobody edits generated code for style. Staticcheck's own `//lint:ignore` directives keep working for everything else.

## The modernize selection

`golang.org/x/tools` ships `modernize` as a suite of separately named analyzers, each replacing a hand-written construct with the newer language or library feature that says the same thing. Since Go 1.26, `go fix ./...` applies the whole suite, so every one of its diagnostics has a mechanical fix. That also means a modernize rule only earns a CI failure when the pattern shows up in practice, reads better after the fix, and is not already reported by a rule above.

Measured against this repository's own modules at `golang.org/x/tools v0.50.0`, seven analyzers fired: the five below, plus `embedlit` and `appendclipped`. `rangeint` fired only on the deliberately bad fixture. These five are on:

| Rule | Replaces | Needs |
| --- | --- | --- |
| `minmax` | An assignment followed by an `if` that clamps it, with `min` or `max` | Go 1.21 |
| `mapsloop` | A loop that copies every entry of one map into another, with `maps.Copy` | Go 1.23 |
| `slicescontains` | A loop that only looks for one element, with `slices.Contains` | Go 1.21 |
| `stringscutprefix` | `HasPrefix` followed by `TrimPrefix`, with `strings.CutPrefix` | Go 1.20 |
| `stringsseq` | Ranging over `strings.Split` or `strings.Fields`, with `SplitSeq` or `FieldsSeq` | Go 1.24 |

Each rule checks the Go version of the file it looks at, so a module whose `go` directive predates the feature gets no diagnostic rather than a fix it cannot compile. The `modernize-legacy` fixture pins that: it declares `go 1.19`, carries the same five patterns, and must lint clean.

The fix is `go fix` with one flag per enabled rule, never a suppression:

```sh
go fix -minmax -mapsloop -slicescontains -stringscutprefix -stringsseq ./...
```

A bare `go fix ./...` applies the whole suite, including the rewrites listed below as off, so name the rules.

The rest of the suite stays off:

- `rangeint` repeats `intrange`, which is already on.
- `appendclipped`, `bloop`, `fmtappendf`, and `slicesdelete` are excluded from the suite upstream because the rewrite can change nil-ness, skew benchmarks, or make code less clear.
- `embedlit` prefers Go 1.27's flattened literals for promoted fields, which hides which embedded struct a field belongs to. That is a spelling preference, not a simplification with a cost.
- `any`, `atomictypes`, `errorsastype`, `forvar`, `newexpr`, `omitzero`, `plusbuild`, `reflecttypefor`, `slicessort`, `stditerators`, `stringsbuilder`, `stringscut`, `testingcontext`, and `waitgroupgo` never fired here. `go fix` still applies them; a rule with no evidence behind it is not worth a failing build.
- `importcomment`, `reflecttypeassert`, `slicesbackward`, `slicesclip`, and `unsafefuncs` are in the suite but unexported at this release, so the linter cannot register them on their own. `go fix` still applies them.

To revisit the selection after a `golang.org/x/tools` upgrade, run `go fix -diff ./...` in the repository and compare what changed against this list.

## Upstream analyzer settings

Three of the upstream analyzers above need a word about scope:

- `unparam` skips exported functions, its own default. The linter checks one package at a time, so it cannot see callers in other packages, and changing an exported signature would break them. Functions in a `main` package are checked either way, since nothing can import them. A package is also checked without its tests, so a parameter that receives the same value at four or more call sites in non-test code is reported even when a test passes other values.
- `musttag` checks the calls it knows: `encoding/json`, `encoding/xml`, `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`, `github.com/mitchellh/mapstructure`, and `github.com/jmoiron/sqlx`. It skips named struct types declared outside the module that contains the package, found from the nearest `go.mod`, and types that implement the matching marshaler interface. A field tagged with the name it already had, such as `json:"Checksum"`, is the fix that keeps existing data readable. The analyzer skips an argument that is a bare variable name, because Staticcheck's loader parses without the object resolution it uses to tell a variable from `nil`, so `json.Marshal(value)` is not checked while `json.Marshal(&value)`, `json.Unmarshal(data, &value)`, and a composite literal are.
- `recvcheck` keeps its built-in exclusions for `UnmarshalText`, `UnmarshalJSON`, `UnmarshalYAML`, `UnmarshalXML`, `UnmarshalBinary`, and `GobDecode`, which need a pointer receiver even on a type whose other methods take values. Methods declared in generated files do not count: code generators such as Dagger's add a value-receiver `MarshalJSON` to a type whose hand-written methods take pointers, and nobody can change the generated receiver.

## Considered and off

These analyzers were measured against this repository and left out. Counts are findings on Levenshtein's own modules.

| Analyzer | Why it is off |
| --- | --- |
| `noinlineerr` | 175 findings on the idiomatic `if err := f(); err != nil` form; house taste, not a bug |
| `paralleltest` | 122 findings asking every test to call `t.Parallel()`; whether a test can run in parallel is the author's call, and `tparallel` already reports the inconsistent case |
| `err113` | 100 findings asking for package-level sentinel errors instead of errors built in place; house taste |
| `goconst` | 90 findings on repeated string literals; a constant does not make a repeated message or test input clearer |
| `wrapcheck` | 85 findings asking for every error returned from another package to be wrapped; house taste |
| `cyclop` | 50 findings on cyclomatic complexity, a threshold with no bug behind it |
| `testpackage` | 25 findings asking for external `_test` packages; white-box tests are a legitimate choice |
| `gochecknoglobals` | 33 findings, all read-only lookup tables and fixed configuration such as check-kind lists |
| `govet` `shadow` | 13 findings, every one an `err` declared again in a nested scope |
| `gosec` | Its findings were file-permission and `exec` noise, and its taint findings were false alarms |
| `nilnil` | Flags the idiomatic `return nil, nil` in `go/analysis` run functions |
| `forcetypeassert` | Only hit `sync.Map` loads whose type is fixed by construction |
| `unconvert` | A false alarm on a syscall conversion that another OS needs |
| `dupword` | Its findings were intended repeated words |
| `copyloopvar` | Obsolete since Go 1.22 gave each loop iteration its own variable |
| `nolintlint` | Staticcheck already reports a `//lint:ignore` directive that matches nothing |

## Typed choices: LV1001

Fields named `Status`, `State`, `Kind`, `Mode`, or `Executor` (case insensitive) must use a defined type instead of plain `string` or an alias of `string`. Other text fields, such as paths and messages, remain ordinary strings. LV1001 also detects enum-like usage regardless of the name, for fields, parameters, and local variables:

- A switch on a string variable or field with at least two distinct nonempty constant choices.
- An OR-chain of equality comparisons, or an AND-chain of inequality comparisons, against at least two distinct nonempty constant choices for the same variable or field.

For example, `priority == "high" || priority == "low"` requires a defined type and typed constants. A single special-case comparison such as `filename == "README.md"` does not. Empty-string checks do not count as enum alternatives. Named string constants and constant expressions count too. These patterns also apply to defined string types with no package-level constants: adding only `type Priority string` does not bypass the requirement for typed constants. References to constants of the matching defined type, including local constants, are accepted.

These are usage heuristics, not proof of a closed domain. A multi-value filename switch can still need a suppression. Calls, indexed expressions, separate comparisons in unrelated statements, and arbitrary validator functions are not inferred as enums. The diagnostic is attached to the switch or comparison so Staticcheck suppression can explain a legitimate open-ended string domain.

```go
type Status string

const (
    StatusPassed Status = "passed"
    StatusFailed Status = "failed"
)

type Result struct {
    Status Status
}

if result.Status == StatusPassed {
    // ...
}
```

For defined string types with package-level typed constants, the check also rejects nonempty string literals and constant expressions used as values, including comparisons, struct literals, assignments, calls, and returns. Declare spellings in constants. Empty zero values and conversions from runtime input remain allowed; this is not runtime enum validation.

## Construct value records together: LV1002

LV1002 applies to all struct types: named, anonymous, local, imported, and aliases. No annotation is required; the former `//levenshtein:record` marker has no special meaning.

Compute intermediate values first, then construct the struct:

```go
return Result{
    Status:     status,
    VerifiedAt: executed.UTC(),
    Error:      message,
}
```

The analyzer tracks new local values made with a literal, `&T{}`, `new(T)`, or a zero-valued `var`. It reports at the creation/declaration when fields are assigned before the value is first used, including nested value fields and construction in `if` branches. One diagnostic covers a construction sequence.

A read, alias, address escape, call using the value, or compound update ends that construction window. Updating parameters, receivers, values returned by factories, or objects already used remains allowed. This conservative local analysis does not follow aliases or prove effects inside callees. Across loops and other complex control flow it stops tracking referenced outer values, while still checking new values created inside their blocks.

The rule promotes clear initialization, not immutability. Whole-value assignments remain allowed. It does not require listing zero-valued fields or force construction to the end of a function. For unavoidable staged setup, put `//lint:ignore LV1002 <reason>` immediately before the reported declaration.

## One field per line: LV1003

LV1003 reports a struct field declaration that carries more than one name, so `Left, Right string` becomes two lines. The rule covers named, anonymous, local, and embedded struct types. Function parameters and results are untouched; only struct fields have to stand alone. Generated files are skipped.

```go
type Pair struct {
    Left  string
    Right string
}
```

Sharing a type is what makes the shorthand tempting and what makes a later type change easy to miss. Spelling the type twice costs one line and makes each field greppable on its own.

## A blank line between declarations: LV1004

LV1004 reports two adjacent top-level declarations with no blank line between them. A declaration begins at its doc comment, so a comment attached to the second declaration does not satisfy the rule; the blank line goes above the comment. Members of a parenthesized `const`, `var`, or `type` group are one declaration and need no spacing. The import block is exempt because `gofmt` already separates it. Generated files are skipped.

The rule does not ask for more than one blank line, and it says nothing about spacing inside a function body, which stays a judgment call described in [AGENTS.md](../AGENTS.md).

## Formatted files: LV1005

LV1005 compares a file's bytes with what `go/format` produces and reports once per file when they differ. It exists so a consumer gets formatting enforcement from `./verify go-lint` without a separate `gofmt` step in CI. The fix is always plain `gofmt -w`, never a suppression. Generated files are skipped.

## Tests that can fail: LV1006

LV1006 reports a test that passes whatever the code under test does. It looks at each `TestXxx(t *testing.T)` in a `_test.go` file and reports two shapes:

- **No assertion.** Nothing in the body, including subtest literals and cleanup callbacks, can fail the test. A test value counts as a way to fail when it is used as anything other than the receiver of a method that cannot report a failure (`Log`, `Parallel`, `Helper`, `Cleanup`, `TempDir`, `Setenv`, `Skip`, and similar). So `t.Errorf`, `t.Fatal`, `require.Equal(t, ...)`, a helper that receives `t`, a struct that stores `t`, and `t.Run` with a named function all count. An explicit `panic`, `log.Fatal`, `log.Panic`, `os.Exit`, or `runtime.Goexit` counts too. A panic or exit inside code the test calls, such as `regexp.MustCompile`, a third-party logger's `Fatal`, or a local wrapper around `os.Exit`, does not: state what the test expects with an assertion.
- **An unconditional skip.** A `t.Skip`, `t.Skipf`, or `t.SkipNow` statement directly in the test body, with nothing before it that can fail the test or return, runs on every invocation, so nothing is ever checked. A skip inside a branch, such as `if testing.Short()`, is a condition and is allowed, and so is a skip after checks that already ran.

```go
// Reported: the result is computed and logged, never checked.
func TestDouble(t *testing.T) {
    t.Log(Double(2))
}

// Accepted.
func TestDouble(t *testing.T) {
    if got := Double(2); got != 4 {
        t.Errorf("Double(2) = %d, want 4", got)
    }
}
```

The rule is conservative on purpose: any use of `t` the analysis cannot see into counts as a way to fail, so a helper that receives `t` and never uses it is not reported. Benchmarks, fuzz targets, examples, and `TestMain` are out of scope. Generated files are skipped.

LV1006 only proves that a test can fail. It does not prove the test fails when behavior is wrong. The pinned upstream checks catch assertions that compare a value with itself or a constant with a constant: Staticcheck's `SA4000` for identical operands, and testifylint's `useless-assert` for calls such as `assert.Equal(t, x, x)`.

## Development and exceptions

The analyzers use Go's `go/analysis` framework and Staticcheck's runner for package loading, caching, diagnostics, and suppression. Every rule skips generated Go files, the house rules on their own and the upstream analyzers through a shared wrapper that also restores each analyzer's name as the reported code. Analyzer regression fixtures live in `runner/lint/policy/testdata` and run with `cd runner/lint && go test ./...`.

For an exceptional interop requirement, use Staticcheck's normal directive with a reason, for example `//lint:ignore LV1001 external schema requires this field`. Prefer a proper type or record literal when possible.

You can run the same linter directly without Dagger:

```sh
(cd /path/to/levenshtein/runner/lint && go build -o /tmp/levenshtein-lint ./cmd/levenshtein-lint)
cd /path/to/consumer
/tmp/levenshtein-lint -checks='all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022' ./...
```

## Named checks and suggested runs

Runs select checks by name; existing CI still owns triggers and schedules.

| Check | Scope | Suggested use |
| --- | --- | --- |
| `go-lint` | The whole default set above: Staticcheck `SA*`/`S1*`/`ST1*`/`QF1*`/`U1000`, the curated upstream and modernize analyzers, and LV1001–LV1006 | Branch and pre-merge |
| `go-vet` | The pinned Go toolchain's default vet checks | Branch and pre-merge |
| `go-http` | bodyclose alone, for a repo that wants the resource check without the rest | HTTP clients/services |
| `go-sql` | sqlclosecheck alone, for a repo that wants the resource check without the rest | Database users |
| `workflow-lint` | actionlint: GitHub Actions syntax and expressions | Repos with GitHub Actions |
| `go-vuln` | govulncheck: reachable known vulnerabilities | Dependency updates and daily |
| `self-test` | Levenshtein's own good/bad fixtures | Shared-check development |
| `go-mutation` | gremlins mutation testing of the Go files a branch changed; fails when a covered mutant survives ([details](mutation.md)) | Pre-merge, or its own run |
| `semantic-lint` | Advisory Jev judgments about Go comments, errors, tests, docs, and PR shape ([details](semantic-lint.md)) | Pull requests, in its own run |

For example, an HTTP service can compose checks using the current versioned interface:

```json
{
  "version": 1,
  "targets": {
    "api": {"dir": "services/api", "workspace": ".", "inputs": ["services/api", "go.work"]},
    "worker": {"dir": "services/worker", "workspace": ".", "inputs": ["services/worker", "go.work"]}
  },
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "lint": {"kind": "go-lint", "targets": ["api", "worker"], "environment": "go"},
    "http": {"kind": "go-http", "targets": ["api", "worker"], "environment": "go"},
    "audit": {"kind": "go-vuln", "target": "api", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["lint", "http/api"]},
    "dependency-audit": {"checks": ["audit"]},
    "main": {"checks": ["lint", "http", "audit"], "rerun_checks": true}
  }
}
```

Levenshtein's own `levenshtein.json` is a worked example of splitting executors: `branch` and `pre-merge` bind `go-lint`, `go-vet`, and `workflow-lint` to a [native environment](configuration.md#native-go-checks) for speed, and `main` keeps the same kinds in Dagger as the daily hermetic audit.

A check with [`targets`](configuration.md#one-check-several-targets) plans one check per target: `lint` becomes `lint/api` and `lint/worker`, while `http/api` selects a single target. Each Go check runs for its selected target. Use a repository-root target (`dir: "."`) for `workflow-lint`; a workflow-less repo should omit it. A repository without `levenshtein.json` gets these checks over a single whole-tree target. HTTP and SQL checks do not replace application tests. ShellCheck and Pyflakes integration is explicitly disabled so results do not depend on optional host tools.

Go vet and standalone tool failures retain native output, including file/line details, inside the report's diagnostic message. Their outer location identifies the module/root rather than pretending the message was parsed into individual source diagnostics. Tool errors never pass; govulncheck's vulnerability exit code is distinguished from network or tool failures.

### Errors and enum switches

Keep errcheck's upstream exclusions for operations documented never to fail. Intentionally ignored errors require explicit `_ =`, preferably with a reason; do not add broad Close/Write exclusions. Check write/flush/close errors when they affect persisted data. A default switch branch does not satisfy exhaustive; list all declared enum values, or use a narrow justified suppression for intentionally partial switches. These checks do not prove runtime enum validity or that an assigned error is handled.

### Cache and freshness

Dagger shares pinned tool builds, dependency downloads, and compiler caches. Staticcheck retains its own analysis cache. The default selection expands only when a pinned analyzer version changes; review new findings with dependency upgrades.

Vulnerability data can change without source changes. A `go-vuln` check always bypasses the local result cache, even with `cache: true` and in custom runs. The Dagger executor generates a unique nonce before invoking `sharedCheck`; the nonce enters after tool construction, forcing a new advisory lookup and scan while reusing tool builds. Ordinary checks retain their result caches. Direct Dagger callers must supply a unique `nonce` for each vulnerability invocation.

The report does not claim an immutable vulnerability-database snapshot. Network/database failures fail verification. Levenshtein's daily `main` run includes the scan; consumer CI owns its daily and dependency-change triggers. Pinned standalone tools live in `runner/tools/go.mod`, separate from Staticcheck's analysis dependencies in `runner/lint/go.mod`.

### Goroutine leak checks in application tests

Goroutine leaks require runtime tests, not another static analyzer. A consumer can pin `go.uber.org/goleak v1.3.0` and integrate it at package scope:

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

Use imports `testing` and `go.uber.org/goleak`. Package-level verification works with parallel tests; per-test leak checks can mistake other running tests for leaks. Combine this with the repository's normal tests/race tests, and explicitly account for legitimate background goroutines. Levenshtein does not inject TestMain into consumer packages.
