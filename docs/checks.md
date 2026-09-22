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
| `bodyclose`, `sqlclosecheck`, `rowserrcheck`, `noctx`, `contextcheck` | Resources a program opens and never closes, and calls that drop the context ([settings](#upstream-analyzer-settings)) |
| `nilness`, `unusedwrite`, `errorlint`, `nilerr`, `durationcheck`, `reassign`, `wastedassign` | Behavior that is wrong rather than unidiomatic |
| `musttag` | Tag every exported field of a struct passed to a JSON, XML, YAML, or TOML encoder or decoder, so renaming a Go field cannot silently change the format ([settings](#upstream-analyzer-settings)) |
| `recvcheck` | Give a type all pointer or all value receivers; a mix means a value and a pointer have different method sets, and value methods work on a copy |
| `nilnesserr` | Return an error already known to be nil after checking a different one, so a failure reaches the caller as success ([why](#known-bug-patterns)) |
| `fatcontext` | Reassign a context to a child of itself in a loop or function literal, so the chain, and every lookup through it, grows with each pass ([why](#known-bug-patterns)) |
| `appendAssign`, `argOrder`, `badCall`, `badCond`, `badRegexp`, `codegenComment`, `deprecatedComment`, `dupArg`, `dupBranchBody`, `dupCase`, `exitAfterDefer`, `filepathJoin`, `flagDeref`, `flagName`, `mapKey`, `offBy1` | go-critic's likely-bug checks that no rule above already reports ([selection](#the-go-critic-selection)) |
| `zerologlint`, `loggercheck` | Log calls that lose what they record: a zerolog event never sent, and a key without a value for logr, klog, zap, or go-kit log ([why](#known-bug-patterns), [settings](#upstream-analyzer-settings)) |
| `bidichk`, `gocheckcompilerdirectives` | Source that runs differently than it reads: Unicode bidirectional controls that reorder how a line displays, and `//go:` directives the toolchain silently ignores ([why](#known-bug-patterns)) |
| `unparam` | Unexported functions with a parameter no caller needs, a parameter that always receives the same value, or a result no caller uses |
| `intrange`, `usestdlibvars`, `perfsprint`, `predeclared`, `errname` | Modern, consistent standard-library usage |
| `exptostd` | `golang.org/x/exp` functions and constraints that the standard library now provides; x/exp makes no compatibility promise ([why](#known-bug-patterns)) |
| `minmax`, `mapsloop`, `slicescontains`, `stringscutprefix`, `stringsseq` | Hand-written loops and comparisons that one standard-library call replaces; `go fix` applies the fix |
| `thelper`, `tparallel`, `testifylint` | Mistakes that only appear in `_test.go` files |
| `usetesting` | A test that changes the working directory or environment, or creates a temporary file or directory, in a way that outlives it ([why](#known-bug-patterns), [settings](#upstream-analyzer-settings)) |
| `gocognit` | Off by default, opt-in: a function whose cognitive complexity is over 30 ([opt in](#opt-in-complexity-gocognit)) |
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

`all` also selects the bare analyzers compiled into the binary, so `errcheck`, `exhaustive`, the resource and correctness analyzers, and the `LV*` rules are part of it. The selection then turns one of those off again, `-gocognit`, which is [opt-in](#opt-in-complexity-gocognit). Someone running the linter directly can pass their own selection; the same `-checks` syntax applies, a `-` prefix removes a rule a wider pattern selected, and a later name turns a removed rule back on.

Upstream analyzers run over generated files, so the facts they export stay correct, but their diagnostics there are dropped: nobody edits generated code for style. In a package that imports `"C"`, the analyzers see cgo's rewrite of each file, which cgo marks generated; every rule judges such a file by the original it maps back to, so hand-written cgo files are checked and reported at their own paths, while cgo's own additions such as `_cgo_gotypes.go` count as generated. Staticcheck's own `//lint:ignore` directives keep working for everything else.

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

## The go-critic selection

[go-critic](https://go-critic.com/overview.html) tags each of its checkers. The `diagnostic` tag marks likely bugs; `style`, `performance`, and `opinionated` are advice, and `experimental` marks checkers upstream has not settled. The linter runs the stable `diagnostic` checkers, minus four that repeat a rule already on, plus two experimental ones that nothing else covers.

Each checker is registered as its own analyzer, so a finding's code is the checker's name, as with the modernize rules. `//lint:ignore offBy1 reason` silences one checker on one line, and `-checks='all,...,-offBy1'` turns one off; there is no single `gocritic` code or glob that covers them all.

| Checker | Reports |
| --- | --- |
| `appendAssign` | `x = append(y, ...)` where `y` is not `x`, usually a copy-paste slip |
| `argOrder` | Arguments that look swapped, such as `strings.HasPrefix("#", line)` |
| `badCall` | A call that does nothing useful, such as `strings.SplitN(s, sep, 0)` or `filepath.Join` of one element |
| `badCond` | A condition that is always true or false, or a loop condition that points the wrong way |
| `badRegexp` | A valid regular expression with a likely mistake, such as a repeated character in a class; experimental upstream |
| `codegenComment` | A generated-file comment tools do not recognize, so the file is linted and reviewed as hand-written |
| `deprecatedComment` | A deprecation notice not written as `Deprecated: `, which tools and pkg.go.dev then miss |
| `dupArg` | The same argument twice where that is a no-op, such as `copy(dst, dst)` |
| `dupBranchBody` | An `if` whose two branches are identical |
| `dupCase` | A `switch` case listed twice, which can never match the second time |
| `exitAfterDefer` | `log.Fatal` or `os.Exit` in a function with deferred calls that will not run |
| `filepathJoin` | A path separator inside one `filepath.Join` element; experimental upstream, and it found two in Levenshtein's own tests |
| `flagDeref` | Dereferencing a `flag` pointer at definition, which reads the default instead of the parsed value |
| `flagName` | A flag name with whitespace that no command line can pass |
| `mapKey` | A map literal key with stray whitespace next to keys without it |
| `offBy1` | Indexing a slice at its length, which always panics |

These stable diagnostic checkers are off because a rule already on reports the same line. Each was confirmed on a sample where both fire:

| Checker | Already reported by |
| --- | --- |
| `caseOrder` | `SA4020`, an unreachable case in a type switch |
| `dupSubExpr` | `SA4000`, identical operands on both sides of `==`, `-`, `&&`, and the other binary operators |
| `sloppyLen` | `SA4024` for `len(x) < 0`; its other finding, `len(x) <= 0`, is a spelling preference |
| `sloppyTypeAssert` | `S1040`, a type assertion to the type the value already has |

`badCall` overlaps in part: `SA1018` also reports `strings.Replace` with a count of zero, and `SA4021` a single-argument `append`, so those two lines get a finding from each. It stays on because nothing else reports its `SplitN` and one-element `filepath.Join` cases.

The other experimental checkers stay off. Five of them looked like candidates and were checked one by one: four repeat a rule already on, confirmed on a sample where both fire, and one is taste.

| Checker | Why it is off |
| --- | --- |
| `builtinShadowDecl` | `predeclared` reports the same declaration |
| `externalErrorReassign` | `reassign` reports the same assignment |
| `nilValReturn` | `nilerr` reports the same `return err` inside `if err == nil` |
| `dynamicFmtString` | `SA1006` reports the same call, and `go vet`'s printf check does too |
| `sloppyReassign` | Taste: it asks for `err :=` in place of `err =`, which can introduce shadowing |

To revisit the selection after a go-critic upgrade, run the linter test in `runner/lint` (`TestCriticSelection` pins the list) and compare the new checkers against this page.

## Known bug patterns

The rest of this page holds a rule to one bar: it is on because it was measured to fire on real code, here or in the fixtures that stand in for a consumer. Eight analyzers are an explicit exception. None of them found anything in Levenshtein's own modules when they were added; each is on because the pattern it reports is a known bug, not a matter of style, its false alarms are rare, and its fix is local:

| Analyzer | Why it earns a failing build |
| --- | --- |
| `nilnesserr` | `if err2 != nil { return err }`, after `err` was already checked, returns nil, so the caller carries on as if the operation had worked. `nilerr` covers the related `return err` inside `if err == nil`; this is the copy-paste slip between two error variables |
| `fatcontext` | `ctx = context.WithValue(ctx, ...)` in a loop wraps the previous iteration's context, so memory and every `Value` lookup grow with the loop count. The fix is a new variable scoped to the iteration |
| `bidichk` | A Unicode bidirectional control character, anywhere in a file, can make code display in a different order than the compiler reads it, the "Trojan Source" attack (CVE-2021-42574). `ST1018` reports these characters in string literals only; `bidichk` also covers comments. Go source has no legitimate need for one that an escape sequence cannot serve |
| `gocheckcompilerdirectives` | A misspelled `//go:` directive, or one written as `// go:` with a space, is an ordinary comment to the compiler and to `go generate`, so an intended `noinline`, `linkname`, `embed`, or `generate` silently does nothing |
| `exptostd` | `golang.org/x/exp` is experimental and has already changed signatures under its callers, such as `slices.SortFunc` moving from a less function to a comparison function. The standard-library `slices`, `maps`, and `cmp` replacements keep the Go 1 compatibility promise. It reports nothing in a module that does not import x/exp |
| `zerologlint` | A zerolog event writes nothing until `Msg`, `Msgf`, `MsgFunc`, or `Send` dispatches it, so `log.Error().Err(err)` without one compiles, runs, and drops the line, usually on the error path where it was needed. It reports nothing in a module that does not use zerolog |
| `loggercheck` | Structured loggers take their fields as alternating keys and values in a `...any` parameter, so the compiler accepts a key with no value. The logger then records the field with a placeholder or an error in its place, and a forgotten key moves every later value under the wrong name. It reports nothing in a module that uses none of the loggers it knows |
| `usetesting` | `os.Setenv` and `os.Chdir` in a test change process state that every later test in the package sees, and `os.MkdirTemp` and `os.CreateTemp("", ...)` leave files behind. `t.Setenv`, `t.Chdir`, and `t.TempDir` undo the change when the test ends |

Three more analyzers were added on the same argument and then left out, because they cannot find their bug under this linter:

- `asasalint` reports a `[]any` passed as a single argument to a `...any` parameter. At v0.0.11 it recognizes the call only when both the parameter and the slice are spelled with `interface{}`: since Go 1.23 `any` is an alias type, and the check does not unwrap it, so a call that spells either one `any` is never reported (golangci-lint's build misses it the same way).
- `makezero` reports an `append` to a slice made with a non-zero length. It tracks the slice through the parser's object resolution, which Staticcheck's loader turns off, so under this linter it never reports anything and prints a warning for every `append`.
- `spancheck` reports an OpenTelemetry or OpenCensus span that is not ended on every path. At v0.6.5 it matches `span.End()` to the span through the same object resolution, so under this linter it reports every span, including one closed with `defer span.End()`, and it crashes on a function that starts a second span into the same variable.

## Upstream analyzer settings

These upstream analyzers need a word about scope or settings:

- `unparam` skips exported functions, its own default. The linter checks one package at a time, so it cannot see callers in other packages, and changing an exported signature would break them. Functions in a `main` package are checked either way, since nothing can import them. A package is also checked without its tests, so a parameter that receives the same value at four or more call sites in non-test code is reported even when a test passes other values.
- `musttag` checks the calls it knows: `encoding/json`, `encoding/xml`, `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`, `github.com/mitchellh/mapstructure`, and `github.com/jmoiron/sqlx`. It skips named struct types declared outside the module that contains the package, found from the nearest `go.mod`, and types that implement the matching marshaler interface. A field tagged with the name it already had, such as `json:"Checksum"`, is the fix that keeps existing data readable. The analyzer skips an argument that is a bare variable name, because Staticcheck's loader parses without the object resolution it uses to tell a variable from `nil`, so `json.Marshal(value)` is not checked while `json.Marshal(&value)`, `json.Unmarshal(data, &value)`, and a composite literal are.
- `contextcheck` reports a function that has a context but calls something that starts its own, directly or through a chain of calls, so cancelling the caller does not stop the work. It follows those chains within one package only: Staticcheck's runner hands every package fact to its own analyzers regardless of type, and they panic on the facts contextcheck exports, so it runs with facts off. A call into another package that makes its own context is missed rather than misreported. Its known false alarms are work that is meant to outlive the caller, such as a cleanup after cancellation or a goroutine that finishes a request's side effects: derive that context with `context.WithoutCancel(ctx)`, which the check accepts and which keeps the caller's values, or use `//lint:ignore contextcheck reason` on the call. A function that returns a context is treated as a constructor and not reported, and an HTTP handler is treated as having the request's context.
- `recvcheck` keeps its built-in exclusions for `UnmarshalText`, `UnmarshalJSON`, `UnmarshalYAML`, `UnmarshalXML`, `UnmarshalBinary`, and `GobDecode`, which need a pointer receiver even on a type whose other methods take values. Methods declared in generated files do not count: code generators such as Dagger's add a value-receiver `MarshalJSON` to a type whose hand-written methods take pointers, and nobody can change the generated receiver.
- `fatcontext` checks loops and function literals, its defaults. Its `check-struct-pointers` mode stays off: upstream marks it a potential finding, and it cannot tell a context stored in a struct for later use from one that grows.
- `usetesting` reports `os.Chdir`, `os.Setenv`, `os.MkdirTemp`, and `os.CreateTemp` with an empty directory inside a function that takes a `*testing.T`, `*testing.B`, or `testing.TB`, and `os.Chdir` only in packages whose `go` version has `t.Chdir` (1.24). `os.Setenv` is on here although upstream leaves it off, because a variable left set is the likeliest of the four to change another test's result. Its `context.Background`, `context.TODO`, and `os.TempDir` detections stay off: `t.Context()` is a better spelling, but a background context in a test changes nothing another test sees, and `os.TempDir` only names a directory.
- `bidichk` reports all nine bidirectional control characters, its default.
- `gocheckcompilerdirectives` checks a directive that has an argument after it, such as `//go:generate stringer` or `//go:linkname local remote`. A directive with nothing after it, such as a misspelled `//go:noinline`, is not checked.
- `zerologlint` follows an event through branches and into a function it is passed to, one call deep. A function that returns a `*zerolog.Event` for its caller to finish is reported, because nothing dispatches the event inside it; mark such a builder with `//lint:ignore zerologlint reason`.
- `loggercheck` checks logr, `k8s.io/klog/v2` (`InfoS`, `ErrorS`, and their variants), zap's sugared `With` and `...w` methods, and go-kit log, which is off upstream and on here because an odd key-value list is the same bug there. It skips a call that spreads a slice with `...`. `log/slog` is left to go vet's `slog` check, which reports the same missing value and a key that is not a string, so a slip in a `slog` call is one finding in `go-vet` rather than one in each check. A configuration that runs `go-lint` without `go-vet` gets no report of it. Its `requirestringkey` and `noprintflike` options stay off: a key held in a variable is fine, and a `%` in a message is not always a format verb. Its report that a nil pointer to a `fmt.Stringer` may panic is dropped: zap, klog, logr's `funcr`, and `fmt` all recover from a `String` method that panics on a nil receiver.
- `exptostd` suggests a replacement only when the module's `go` version has it: Go 1.21 for most of `slices` and `maps`, and Go 1.23 for `maps.Keys` and `maps.Values`, which return iterators in the standard library.

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
| `asasalint` | Misses its bug when the parameter or the slice is spelled with `any` ([details](#known-bug-patterns)) |
| `makezero` | Never reports under Staticcheck's loader, and prints a warning for every `append` ([details](#known-bug-patterns)) |
| `sloglint` | Its only bug-adjacent option, `no-mixed-args`, is a consistency rule: `slog` handles key-value pairs and attributes mixed in one call correctly. It also always suggests `slog.DiscardHandler` over a handler writing to `io.Discard`, a modernization that cannot be turned off |
| `spancheck` | Reports every span as never ended under Staticcheck's loader, and crashes on a reused span variable ([details](#known-bug-patterns)) |

## Opt-in complexity: gocognit

`gocognit` reports a function whose [cognitive complexity](https://github.com/uudashr/gocognit#cognitive-complexity) is over 30. Each `if`, loop, `switch`, `select`, jump, and run of mixed `&&`/`||` adds one, plus one more for each level of nesting it sits in, so the score tracks how much a reader has to hold in mind rather than how many paths a test needs. The binary registers it, and the shipped selection turns it off with `-gocognit`, because a threshold says a function is hard to maintain, not that it is wrong: it would fail builds on working code, which is the bar every default rule is held to.

The threshold is fixed at 30, golangci-lint's default and the common line past which a function is hard to maintain. A shared linter has no per-repository settings, so a consumer that wants a different line cannot set one. Levenshtein does not opt itself in. Measured with `-checks=gocognit`, nine of its functions are over the line, which shows what the rule asks for:

| Function | Complexity |
| --- | --- |
| `checkConstruction` in `runner/lint/policy/records.go` | 90 |
| `checkEnumUsage` in `runner/lint/policy/enum_usage.go` | 65 |
| `runTypedValues` in `runner/lint/policy/typed.go` | 55 |
| `buildRequests` in `internal/semantic/check.go` | 41 |
| `validateDaggerSource` in `internal/verify/source.go` | 36 |
| `CachedExecutor.Execute` in `internal/verify/cache.go` | 33 |
| `Config.planCheck` in `internal/verify/plan.go` | 33 |
| `request.fit` in `internal/semantic/check.go` | 32 |
| `TestDaggerSourceRejectsAliasesButDoesNotInspectExcludedTrees` in `internal/verify/source_test.go` | 31 |

The `go-lint` check always runs the shipped selection; `levenshtein.json` has no setting that changes which rules it reports. To opt in, run the linter directly, as in [Development and exceptions](#development-and-exceptions), with the shipped selection minus its `-gocognit`, or with `-checks=gocognit` for this rule alone. A finding can be suppressed like any other, with `//lint:ignore gocognit <reason>` above the function.

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

LV1005 compares a file's bytes with what `go/format` produces and reports once per file when they differ. It exists so a consumer gets formatting enforcement from `./verify go-lint` without a separate `gofmt` step in CI. The fix is always plain `gofmt -w`, never a suppression. Generated files are skipped. For a cgo file it checks the original source, not cgo's rewrite in the build cache.

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

You can run the same linter directly without Dagger. The selection below is the shipped default; drop `-gocognit` from it to [opt in to gocognit](#opt-in-complexity-gocognit):

```sh
(cd /path/to/levenshtein/runner/lint && go build -o /tmp/levenshtein-lint ./cmd/levenshtein-lint)
cd /path/to/consumer
/tmp/levenshtein-lint -checks='all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022,-gocognit' ./...
```

## Named checks and suggested runs

Runs select checks by name; existing CI still owns triggers and schedules.

| Check | Scope | Suggested use |
| --- | --- | --- |
| `go-lint` | The whole default set above: Staticcheck `SA*`/`S1*`/`ST1*`/`QF1*`/`U1000`, the curated upstream and modernize analyzers, and LV1001–LV1006 | Branch and pre-merge |
| `go-vet` | The pinned Go toolchain's default vet checks | Branch and pre-merge |
| `go-mod` | `go mod tidy -diff` and `go mod verify`: manifests tidy, downloads matching `go.sum` ([details](#module-manifests)) | Branch and pre-merge |
| `go-test` | `go test -race ./...` on the pinned toolchain: a failing test or a detected data race fails, a test that does not build is an error ([details](#tests)) | Its own run, for self-contained unit tests |
| `go-http` | bodyclose alone, for a repo that wants the resource check without the rest | HTTP clients/services |
| `go-sql` | sqlclosecheck alone, for a repo that wants the resource check without the rest | Database users |
| `workflow-lint` | actionlint: GitHub Actions syntax and expressions | Repos with GitHub Actions |
| `workflow-security` | zizmor's offline audits of workflows, composite actions, and Dependabot configuration, failing at medium severity and above ([details](#workflow-security)) | Repos with GitHub Actions, in its own run |
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

Levenshtein's own `levenshtein.json` is a worked example of splitting executors: `branch` and `pre-merge` bind `go-lint`, `go-vet`, `go-mod`, `workflow-lint`, and `workflow-security` to a [native environment](configuration.md#native-go-checks) for speed, and `main` keeps the same kinds in Dagger as the daily hermetic audit. Its `go-mod` check covers the root module, `runner/lint`, and `runner/tools`, but not `runner`, whose `go.mod` `dagger develop` rewrites on every regeneration.

A check with [`targets`](configuration.md#one-check-several-targets) plans one check per target: `lint` becomes `lint/api` and `lint/worker`, while `http/api` selects a single target. Each Go check runs for its selected target. Use a repository-root target (`dir: "."`) for `workflow-lint` and `workflow-security`; a workflow-less repo should omit them. A repository without `levenshtein.json` gets these checks over a single whole-tree target. HTTP and SQL checks do not replace application tests. ShellCheck and Pyflakes integration is explicitly disabled so results do not depend on optional host tools.

Go vet and standalone tool failures retain native output, including file/line details, inside the report's diagnostic message. Their outer location identifies the module/root rather than pretending the message was parsed into individual source diagnostics. Tool errors never pass; govulncheck's vulnerability exit code is distinguished from network or tool failures.

### Module manifests

`go-mod` runs `go mod tidy -diff`, then `go mod verify`, in the target module. A module whose `go.mod` or `go.sum` differs from what `go mod tidy` would write fails with tidy's own diff as the finding; so does a downloaded dependency that no longer matches the hash recorded when it was fetched, or a download that disagrees with `go.sum` (Go's `SECURITY ERROR`). Both commands exit 1 for a finding and for a failure alike, so the output decides: anything else, such as an unreachable module proxy or a private module the check cannot fetch, is an error, never a pass. The check loads no packages, so a module that only pins tools is still checked; a target without a `go.mod` is an error.

- **Workspaces.** `go-mod` always runs with `GOWORK=off`. `go mod tidy` checks one module's own manifests whatever workspace it belongs to, and `go mod verify` then covers that module's requirements rather than every workspace member's. Give each workspace module its own target.
- **Vendored modules.** Neither command reads `vendor/`: tidy resolves from `go.mod`, `go.sum`, and the module proxy, so `go-mod` needs the proxy (and, natively, any `GOPRIVATE`/`GOPROXY`/credential settings passed with `pass_env`) even for a module that vendors its dependencies. The Dagger container has no private-module credentials. The go command already refuses a `vendor/modules.txt` that disagrees with `go.mod` when `go-vet` or `go-lint` loads packages. A repository that must verify offline leaves `go-mod` out of its runs.
- **Caching.** `go mod verify` checks the module cache as it is now, which no input fingerprint covers, so like `go-vuln` a `go-mod` result is never reused, on either executor, and each Dagger call gets a fresh nonce; a direct Dagger `sharedCheck` call for `go-mod` without one is refused. With the modules already downloaded it takes a few seconds.

Without a `levenshtein.json`, `go-mod` is part of the default `branch`, `pre-merge`, and `main` runs over the root module.

### Tests

`go-test` runs `go test -race -json -vet=off -timeout=10m ./...` in the target module, on the pinned Go toolchain with cgo on, because the race detector needs it. It is not in the default `branch`, `pre-merge`, or `main` gates: a repository without `levenshtein.json` can run it by name (`verify go-test`), and a configured one adds it to a run of its own.

- **Findings and errors.** `go test` exits 1 both when a test fails and when a package does not build, so the check reads `go test -json`'s events rather than the exit code or text that a test's own output could imitate. A package that fails after its tests ran is a finding carrying that package's `go test` output, less the tests in it that passed: a failing or panicking test, a data race the race detector reported (`WARNING: DATA RACE` and `race detected during execution of test`), or a test that hit the timeout. A test that failed is a finding even when `go test` exits 0, as it does when a `TestMain` ignores `m.Run`'s result and exits 0. A package whose test binary could not be built or set up (`[build failed]`, `[setup failed]`) is an error, since its tests never ran, even if another package's tests failed; so is a target without a `go.mod` or without packages, an exit 1 with no failed package, any other exit code, and a module where no test passed because none ran or every one skipped. The report keeps `go test`'s text output.
- **Vet.** `go test` normally runs a subset of `go vet` first and reports its diagnostics as a build failure. The check turns that off with `-vet=off`: those diagnostics are [`go-vet`](#named-checks-and-suggested-runs)'s job, and here they would make the check an error instead of a finding.
- **Timeouts.** `-timeout=10m` is named on the command line, so `GOFLAGS` cannot lift it. A test binary still running after ten minutes panics with every goroutine's stack, which fails its package as a finding. The whole `go test` invocation is bounded by thirty minutes on either executor, the limit the other shared Go checks share; reaching it is an error, and a native check's processes are stopped.
- **cgo.** The Dagger path runs in the pinned `golang` Debian image, which ships `gcc`, with `CGO_ENABLED=1`. The native path sets `CGO_ENABLED=1` whatever the environment says and first looks up the C compiler `go env CC` names (`gcc` on Linux, `clang` on macOS, unless `CC` is set) on the check's `PATH`; without one the check is an error that says so, rather than a pass or a build failure in every package.
- **Caching.** A `go-test` verdict is reused like `go-vet`'s: its key covers the target's declared inputs, the toolchain, and the shared implementation, and only a passing result is stored. A fresh run (`rerun_checks`) passes `-count=1`, so neither Levenshtein's cache nor `go test`'s own reuses anything; any other run lets `go test` reuse the packages whose inputs it can see did not change. That is the right trade-off only for tests whose outcome depends on the declared inputs alone, so declare everything the tests read, including testdata and golden files outside the target directory, in the target's `inputs`. A test that reads the network, the clock, or state outside the repository can pass once and be reused; such tests belong in a `command` check, below.
- **Workspaces.** The check sees the same `go.work` as `go-vet` does, following the target's declared inputs.

`go-test` is for self-contained unit tests: it runs every package's tests in one invocation with nothing but the source, the module cache, and the Go toolchain. Use a [`command` check](configuration.md#native-commands) instead when the tests need anything else:

- **Services.** Tests that need a database, a message queue, or another service need that service started, seeded, and torn down around them, which a `command` check's own script (with a [preparation](configuration.md#native-commands) for any shared setup) can do on a native worker and `go-test` cannot.
- **Flags and scope.** Build tags, `-short`, `-run` filters, a subset of packages, coverage profiles, or `-count` for flake hunting are all `command` arguments; `go-test` takes no options.
- **Live state.** A `command` check leaves result caching off unless it opts in with `cache: true`, so tests that reach the network or other external state are re-executed on every run; give `go test` `-count=1` there so its own cache does not answer either.

A repository whose CI already runs `go test -race` over the same modules gains nothing from also adding `go-test` to that job's run. Levenshtein itself is one: its CI `tests` job runs `go test -race ./...` over the root module and `runner/lint` on every event, so its `levenshtein.json` defines a native `go-test` run over the whole repository and `runner/lint` for local use and leaves it out of `branch`, `pre-merge`, and `main`. `runner`, whose tests need a Dagger session, is not a target.

### Workflow security

`workflow-security` runs [zizmor](https://docs.zizmor.sh) 1.30.1 over the repository's GitHub Actions: every `.github/workflows/*.yml` and `*.yaml`, a composite action at the root (`action.yml` or `action.yaml`) and every `action.yml`/`action.yaml` under `.github/actions`, and `.github/dependabot.yml`. It uses zizmor's default `regular` persona and fails on a finding of medium severity or above, with zizmor's own report, file, line, and audit links included, as the finding. Like `workflow-lint`, it is not in the default `branch`, `pre-merge`, or `main` gates: a repository without `levenshtein.json` can run it by name (`verify workflow-security`), and a configured one adds it to its own runs the way Levenshtein does.

- **Offline audits only.** The check runs `zizmor --offline`, so its verdict depends only on those files, the configuration, and the pinned binary. That makes it deterministic, lets a result be reused like any other shared check's, and means it needs no GitHub token. The audits that query GitHub (`impostor-commit`, `known-vulnerable-actions`, `ref-confusion`, and the pedantic `stale-action-refs`) are left to [zizmor's GitHub Action](https://github.com/zizmorcore/zizmor-action), which can pass the workflow token and run on a schedule so a newly published advisory is noticed without a code change. Levenshtein's own `security.yml` keeps that job for exactly this reason.
- **Configuration.** Like `workflow-lint` with `actionlint.yaml`, the check honors one zizmor configuration file at the root: `.github/zizmor.yml`, `.github/zizmor.yaml`, `zizmor.yml`, or `zizmor.yaml`, the names zizmor itself discovers there. More than one is an error. Without one the check passes `--no-config`, so a file outside the repository, or `ZIZMOR_CONFIG` on a native host, never changes the result; natively, `ZIZMOR_*` settings and GitHub tokens are removed from the environment. Both executors audit only files under the target's `inputs` and outside its `exclude`, so declare the configuration there with the workflows and actions: an undeclared file is not read, and a change to a declared one invalidates a cached result.
- **Exit codes.** zizmor exits 13 or 14 when its most severe finding is medium or high; those are findings. With `--min-severity=medium` nothing lower is reported, so any other nonzero exit (1 for an audit error, 2 for a usage error, 3 for no inputs) is an error, never a pass. `--strict-collection` makes a workflow zizmor cannot parse an error rather than a skipped file. A repository with none of the files above is an error too.
- **The binary.** zizmor is a Rust program, so it is not built from `runner/tools/go.mod`. `runner/toolchain.json` pins the upstream release archive for Linux and macOS on amd64 and arm64 by SHA-256. The Dagger path fetches the archive for the engine's platform with that checksum, which the engine enforces before the file is used; the native path downloads it into its tool directory (under `--cache-dir` when one is set, so it is fetched once), checks the hash before extracting, and hashes the kept archive again on every later run. A platform without a pinned archive is an error, never an unverified download. Both paths need to reach `github.com` to fetch it.

### Errors and enum switches

Keep errcheck's upstream exclusions for operations documented never to fail. Intentionally ignored errors require explicit `_ =`, preferably with a reason; do not add broad Close/Write exclusions. Check write/flush/close errors when they affect persisted data. A default switch branch does not satisfy exhaustive; list all declared enum values, or use a narrow justified suppression for intentionally partial switches. These checks do not prove runtime enum validity or that an assigned error is handled.

### Cache and freshness

Dagger shares pinned tool builds, dependency downloads, and compiler caches. Staticcheck retains its own analysis cache. The default selection expands only when a pinned analyzer version changes; review new findings with dependency upgrades.

Vulnerability data can change without source changes. A `go-vuln` check always bypasses the local result cache, even with `cache: true` and in custom runs. The Dagger executor generates a unique nonce before invoking `sharedCheck`; the nonce enters after tool construction, forcing a new advisory lookup and scan while reusing tool builds. Ordinary checks retain their result caches. Direct Dagger callers must supply a unique `nonce` for each vulnerability or `go-mod` invocation; `sharedCheck` refuses either without one.

The report does not claim an immutable vulnerability-database snapshot. Network/database failures fail verification. Levenshtein's daily `main` run includes the scan; consumer CI owns its daily and dependency-change triggers. Pinned standalone tools live in `runner/tools/go.mod`, separate from Staticcheck's analysis dependencies in `runner/lint/go.mod`.

### Goroutine leak checks in application tests

Goroutine leaks require runtime tests, not another static analyzer. A consumer can pin `go.uber.org/goleak v1.3.0` and integrate it at package scope:

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

Use imports `testing` and `go.uber.org/goleak`. Package-level verification works with parallel tests; per-test leak checks can mistake other running tests for leaks. Combine this with the repository's normal tests/race tests, and explicitly account for legitimate background goroutines. Levenshtein does not inject TestMain into consumer packages.
