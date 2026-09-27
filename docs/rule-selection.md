# Rule selection

A rule turns on for every repository that bumps its Levenshtein pin, so each one has to earn its place. This page is the evidence: how rules are chosen, what was measured, and every analyzer that was considered and left off, with the reason. [Go lint rules](rules.md) lists what is on. To propose a rule, see [proposing a new rule](../CONTRIBUTING.md#proposing-a-new-rule).

## The bar for a rule

A rule is on because it was measured to fire on real code: in Levenshtein's own modules, in the fixtures that stand in for a consumer, or in the [open-source codebases measured below](#measured-on-other-codebases). Each finding is read and judged a bug, taste, or a false alarm. A rule whose findings are mostly taste, such as naming, comment wording, or one of two equally clear spellings, stays off, and so does one whose false alarms outnumber its bugs. A rule whose threshold is a judgment call, such as a complexity limit, can ship registered but off, for repositories to [opt in](rules.md#opt-in-complexity-gocognit) to. An established `go/analysis` analyzer or go-critic checker is preferred over a new house rule, and one that repeats a rule already on stays off so a mistake is reported once.

The [known bug patterns](rules.md#known-bug-patterns) are an explicit exception. None of them found anything in Levenshtein's own modules when they were added, and the later ones found nothing in the other codebases either; each is on because the pattern it reports is a known bug, not a matter of style, its false alarms are rare, and its fix is local.

Three more analyzers were added on the same argument and then left out, because they cannot find their bug under this linter:

- `asasalint` reports a `[]any` passed as a single argument to a `...any` parameter. At v0.0.11 it recognizes the call only when both the parameter and the slice are spelled with `interface{}`: since Go 1.23 `any` is an alias type, and the check does not unwrap it, so a call that spells either one `any` is never reported (golangci-lint's build misses it the same way).
- `makezero` reports an `append` to a slice made with a non-zero length. It tracks the slice through the parser's object resolution, which Staticcheck's loader turns off, so under this linter it never reports anything and prints a warning for every `append`.
- `spancheck` reports an OpenTelemetry or OpenCensus span that is not ended on every path. At v0.6.5 it matches `span.End()` to the span through the same object resolution, so under this linter it reports every span, including one closed with `defer span.End()`, and it crashes on a function that starts a second span into the same variable.

## Measured on other codebases

Levenshtein's own modules are too small a sample to show how often a rule fires or raises false alarms in a typical Go repository, so candidates for the default selection were also run over this repository and eight widely used open-source codebases: spf13/cobra, go-chi/chi, gin-gonic/gin, etcd-io/bbolt, restic/restic, prometheus/node_exporter, caddyserver/caddy, and hashicorp/nomad, at their default branches in September 2026. One nomad package ran the compiler out of memory and was not checked. Each finding was read and judged a bug, taste, or a false alarm.

| Analyzer | Findings | Result |
| --- | --- | --- |
| `scannererr` | 18, in gin, node_exporter, restic, and nomad | All real. restic's sftp backend stops draining a subprocess's stderr on a line over 64 KiB, which can block the subprocess; node_exporter parses udev properties short with no error; nomad's cgroup mode detection falls through to its default on a read error |
| `testableexamples` | 5, in caddy and cobra | All real: examples that compile and never run |
| `deferInLoop` | 34, in seven of the nine | One leak that matters, in node_exporter's NUMA collector, which holds two files per node open until the function returns; the rest are tests or short, fixed loops, so it is [opt-in](rules.md#opt-in-resources-deferinloop) |

The analyzers listed as [known bug patterns](rules.md#known-bug-patterns) that were added after this measurement found nothing in any of the nine, and the rules the tables below leave off are measured here too.

## Considered and off

These analyzers were measured and left out. Counts are findings on Levenshtein's own modules unless a row names the [other codebases](#measured-on-other-codebases).

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
| `gosec` | Its findings were file-permission and `exec` noise, and its taint findings were false alarms. A narrower set, `G108`, `G110`, `G112`, `G114`, `G201`, `G202`, `G203`, `G305`, `G402`, and `G602`, found 13 things in the [other codebases](#measured-on-other-codebases) (nomad timed out), none a bug: example programs, a test helper already suppressed, a deliberate pprof import, a trusted download, and one false alarm |
| `errchkjson` | Most of its 42 findings in the other codebases were an error that is checked although it cannot happen, and the rest ask to check an error the rules allow discarding with `_ =` |
| `protogetter` | 414 findings in the other codebases, all direct field access where a getter would guard against nil; house taste |
| `canonicalheader` | 28 findings in the other codebases, all non-canonical keys passed to `Header.Get` or `Header.Set`, which canonicalize them anyway |
| `deepequalerrors` | Its non-test findings in the other codebases compared configuration structs that happen to contain an error field, not errors |
| `sortslice` | `SA1028` reports the same `sort.Slice` call on a value that is not a slice |
| `sqlrowserr` | `rowserrcheck` reports the same unchecked `Rows.Err` |
| `ineffassign` | `SA4006` and `wastedassign` report the same assignments; it found nothing new |
| `asciicheck` | Found nothing; `bidichk` covers the non-ASCII characters that change how code reads |
| `mirror` | Allocation advice, 2 findings |
| `nilnil` | Flags the idiomatic `return nil, nil` in `go/analysis` run functions |
| `forcetypeassert` | Only hit `sync.Map` loads whose type is fixed by construction |
| `unconvert` | A false alarm on a syscall conversion that another OS needs |
| `dupword` | Its findings were intended repeated words |
| `copyloopvar` | Obsolete since Go 1.22 gave each loop iteration its own variable |
| `nolintlint` | Staticcheck already reports a `//lint:ignore` directive that matches nothing |
| `asasalint` | Misses its bug when the parameter or the slice is spelled with `any` ([details](#the-bar-for-a-rule)) |
| `makezero` | Never reports under Staticcheck's loader, and prints a warning for every `append` ([details](#the-bar-for-a-rule)) |
| `sloglint` | Its only bug-adjacent option, `no-mixed-args`, is a consistency rule: `slog` handles key-value pairs and attributes mixed in one call correctly. It also always suggests `slog.DiscardHandler` over a handler writing to `io.Discard`, a modernization that cannot be turned off |
| `spancheck` | Reports every span as never ended under Staticcheck's loader, and crashes on a reused span variable ([details](#the-bar-for-a-rule)) |

## Modernize analyzers left off

Since Go 1.26, `go fix ./...` applies the whole `modernize` suite, so every one of its diagnostics has a mechanical fix. That also means a modernize rule only earns a CI failure when the pattern shows up in practice, reads better after the fix, and is not already reported by a rule that is on.

Measured against this repository's own modules at `golang.org/x/tools v0.50.0`, seven analyzers fired: the [five that are on](rules.md#the-modernize-selection), plus `embedlit` and `appendclipped`. `rangeint` fired only on the deliberately bad fixture. The rest of the suite stays off:

- `rangeint` repeats `intrange`, which is already on.
- `appendclipped`, `bloop`, `fmtappendf`, and `slicesdelete` are excluded from the suite upstream because the rewrite can change nil-ness, skew benchmarks, or make code less clear.
- `embedlit` prefers Go 1.27's flattened literals for promoted fields, which hides which embedded struct a field belongs to. That is a spelling preference, not a simplification with a cost.
- `any`, `atomictypes`, `errorsastype`, `forvar`, `newexpr`, `omitzero`, `plusbuild`, `reflecttypefor`, `slicessort`, `stditerators`, `stringsbuilder`, `stringscut`, `testingcontext`, and `waitgroupgo` never fired here. `go fix` still applies them; a rule with no evidence behind it is not worth a failing build.
- `importcomment`, `reflecttypeassert`, `slicesbackward`, `slicesclip`, and `unsafefuncs` are in the suite but unexported at this release, so the linter cannot register them on their own. `go fix` still applies them.

## go-critic checkers left off

These stable diagnostic checkers are off because a rule already on reports the same line. Each was confirmed on a sample where both fire:

| Checker | Already reported by |
| --- | --- |
| `caseOrder` | `SA4020`, an unreachable case in a type switch |
| `dupSubExpr` | `SA4000`, identical operands on both sides of `==`, `-`, `&&`, and the other binary operators |
| `sloppyLen` | `SA4024` for `len(x) < 0`; its other finding, `len(x) <= 0`, is a spelling preference |
| `sloppyTypeAssert` | `S1040`, a type assertion to the type the value already has |

The other experimental diagnostic checkers stay off. Each was run over the [codebases measured above](#measured-on-other-codebases). A checker that repeats a rule already on was confirmed on a sample where both fire, and the counts are findings across those codebases:

| Checker | Why it is off |
| --- | --- |
| `builtinShadowDecl` | `predeclared` reports the same declaration |
| `externalErrorReassign` | `reassign` reports the same assignment |
| `nilValReturn` | `nilerr` reports the same `return err` inside `if err == nil` |
| `dynamicFmtString` | `SA1006` reports the same call, and `go vet`'s printf check does too |
| `badSorting` | `SA4029` reports the same `x = sort.StringSlice(x)` |
| `sqlQuery` | `sqlclosecheck` and `rowserrcheck` both report the same discarded `Rows` |
| `sloppyReassign` | Taste: it asks for `err :=` in place of `err =`, which can introduce shadowing |
| `weakCond` | Both findings were false alarms: an index whose bound held by construction, and a `FindSubmatch` result that is never shorter than its groups |
| `badLock` | Its one finding was a test that locks and unlocks on purpose to wait for a reader |
| `dupOption` | All five findings were tests that register the same middleware twice on purpose |
| `sprintfQuotedString`, `commentedOutCode`, `unnecessaryDefer` | Taste: 37, 56, and 1 findings, none a bug |

`emptyDecl`, `regexpPattern`, `sortSlice`, `syncMapLoadAndDelete`, `truncateCmp`, and `uncheckedInlineErr` found nothing in any codebase measured. Several describe real bugs, but nothing yet shows how often they raise false alarms, so they wait for evidence rather than joining on the pattern alone.

## Revisiting the selection

The selection changes only when a pinned analyzer version does, so review new findings with dependency upgrades:

- After a `golang.org/x/tools` upgrade, run `go fix -diff ./...` in the repository and compare what changed against the [modernize list](#modernize-analyzers-left-off).
- After a go-critic upgrade, run the linter tests in `runner/lint` (`TestCriticSelection` pins the list) and compare the new checkers against [the rules that are on](rules.md#the-go-critic-selection) and [the ones left off](#go-critic-checkers-left-off).
