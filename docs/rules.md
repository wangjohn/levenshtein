# Go lint rules

These are the rules `./verify go-lint` enforces. Each one reports under its own code, such as `SA4006`, `errcheck`, or `LV1001`, and every repository that pins a Levenshtein revision gets the same set. A rule is on because it catches bugs or makes code clearer; [rule selection](rule-selection.md) has the evidence behind each choice and the analyzers that were measured and left off. For the other check kinds, which run whole tools rather than rules, see [check kinds](check-kinds.md).

To silence one finding, put `//lint:ignore <code> <reason>` on the line above it; [suppressing a finding](#suppressing-a-finding) has the details.

## Rules on by default

| Rule | Policy |
| --- | --- |
| `SA*` | Correctness checks in the pinned Staticcheck release |
| `S1*` | Simplifications whose result is objectively simpler code |
| `ST1*` | Style rules, minus the six naming and documentation rules [listed below](#the-staticcheck-selection) |
| `QF1*` | Refactorings Staticcheck ships as quick fixes |
| `U1000` | Unused unexported code |
| `errcheck` | Report implicitly discarded errors; explicit `_ =` remains allowed ([policy](#errors-and-enum-switches)) |
| `exhaustive` | Require enum switches to cover declared values ([policy](#errors-and-enum-switches)) |
| `gochecksumtype` | Require type switches over an interface marked `//sumtype:decl` to list every variant; a `default` case does not count ([settings](#upstream-analyzer-settings)) |
| `bodyclose`, `sqlclosecheck`, `rowserrcheck`, `noctx`, `contextcheck` | Resources a program opens and never closes, and calls that drop the context ([settings](#upstream-analyzer-settings)) |
| `nilness`, `unusedwrite`, `errorlint`, `nilerr`, `durationcheck`, `reassign`, `wastedassign` | Behavior that is wrong rather than unidiomatic |
| `musttag` | Tag every exported field of a struct passed to a JSON, XML, YAML, or TOML encoder or decoder, so renaming a Go field cannot silently change the format ([settings](#upstream-analyzer-settings)) |
| `recvcheck` | Give a type all pointer or all value receivers; a mix means a value and a pointer have different method sets, and value methods work on a copy |
| `nilnesserr` | Return an error already known to be nil after checking a different one, so a failure reaches the caller as success ([why](#known-bug-patterns)) |
| `fatcontext` | Reassign a context to a child of itself in a loop or function literal, so the chain, and every lookup through it, grows with each pass ([why](#known-bug-patterns)) |
| `scannererr` | Loop over a `bufio.Scanner` without checking `Err` afterwards, so a read error or an over-long line ends the input early and silently ([evidence](rule-selection.md#measured-on-other-codebases)) |
| `reflectvaluecompare`, `httpmux` | Compare `reflect.Value`s with `==` or `reflect.DeepEqual`, which compares the reflect package's internals, and register a Go 1.22 `ServeMux` pattern in a module whose `go` directive predates it, where it matches nothing ([why](#known-bug-patterns)) |
| `appendAssign`, `argOrder`, `badCall`, `badCond`, `badRegexp`, `badSyncOnceFunc`, `codegenComment`, `deprecatedComment`, `dupArg`, `dupBranchBody`, `dupCase`, `evalOrder`, `exitAfterDefer`, `filepathJoin`, `flagDeref`, `flagName`, `mapKey`, `offBy1`, `rangeAppendAll`, `returnAfterHttpError` | go-critic's likely-bug checks that no rule above already reports ([selection](#the-go-critic-selection)) |
| `zerologlint`, `loggercheck` | Log calls that lose what they record: a zerolog event never sent, and a key without a value for logr, klog, zap, or go-kit log ([why](#known-bug-patterns), [settings](#upstream-analyzer-settings)) |
| `bidichk`, `gocheckcompilerdirectives` | Source that runs differently than it reads: Unicode bidirectional controls that reorder how a line displays, and `//go:` directives the toolchain silently ignores ([why](#known-bug-patterns)) |
| `unparam` | Unexported functions with a parameter no caller needs, a parameter that always receives the same value, or a result no caller uses |
| `intrange`, `usestdlibvars`, `perfsprint`, `predeclared`, `errname` | Modern, consistent standard-library usage |
| `exptostd` | `golang.org/x/exp` functions and constraints that the standard library now provides; x/exp makes no compatibility promise ([why](#known-bug-patterns)) |
| `minmax`, `mapsloop`, `slicescontains`, `stringscutprefix`, `stringsseq` | Hand-written loops and comparisons that one standard-library call replaces; `go fix` applies the fix ([selection](#the-modernize-selection)) |
| `thelper`, `tparallel`, `testifylint` | Mistakes that only appear in `_test.go` files |
| `testableexamples` | An `Example` function without an `// Output:` comment, which `go test` compiles but never runs, so it cannot fail ([evidence](rule-selection.md#measured-on-other-codebases)) |
| `usetesting` | A test that changes the working directory or environment, or creates a temporary file or directory, in a way that outlives it ([why](#known-bug-patterns), [settings](#upstream-analyzer-settings)) |
| `gocognit` | Off by default, opt-in: a function whose cognitive complexity is over 30 ([opt in](#opt-in-complexity-gocognit)) |
| `deferInLoop` | Off by default, opt-in: a `defer` inside a loop, which holds every pass's resource until the function returns ([opt in](#opt-in-resources-deferinloop)) |
| LV1001 | Give enum-like strings defined types and typed constants ([details](#typed-choices-lv1001)) |
| LV1002 | Construct new structs together with literals, without opt-in markers ([details](#construct-value-records-together-lv1002)) |
| LV1003 | Declare each struct field on its own line ([details](#one-field-per-line-lv1003)) |
| LV1004 | Separate top-level declarations with a blank line ([details](#a-blank-line-between-declarations-lv1004)) |
| LV1005 | Keep every file formatted the way `gofmt` writes it ([details](#formatted-files-lv1005)) |
| LV1006 | Give every test a way to fail, and do not skip a test unconditionally ([details](#tests-that-can-fail-lv1006)) |

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

`all` also selects the bare analyzers compiled into the binary, so `errcheck`, `exhaustive`, the resource and correctness analyzers, and the `LV*` rules are part of it. The selection then turns two of those off again, `-gocognit` and `-deferInLoop`, which are opt-in ([gocognit](#opt-in-complexity-gocognit), [deferInLoop](#opt-in-resources-deferinloop)). Someone running the linter directly can pass their own selection; the same `-checks` syntax applies, a `-` prefix removes a rule a wider pattern selected, and a later name turns a removed rule back on.

### Changing the selection for one repository

A `go-lint` check in `levenshtein.json` can add patterns after the shipped selection with a `lint` object. They use the same `-checks` syntax and the last pattern that matches a rule wins, so a name turns an opt-in rule on and a `-` name turns a default rule off, without restating the rest:

```json
"cleanup": {"kind": "go-lint", "target": "app", "environment": "go", "lint": {"checks": ["gocognit", "-unparam"]}}
```

Each entry is one pattern: an optional `-`, then `all`, `*`, a rule name such as `gocognit`, or a name ending in `*` such as `SA5*`. An entry with a comma or a space is a configuration error, and so is `lint` on any other kind: `go-http` and `go-sql` keep their single rule. A pattern that matches no rule the pinned linter registers fails the check with an error rather than doing nothing, so a misspelled opt-in cannot pass silently. Both executors apply the same list, and changing it re-runs the check instead of reusing an earlier result ([configuration](configuration.md#lint-selection)).

The same `lint.checks` list also takes community patterns, such as `errs_nopanic` or `-errs_*`, for rules from a repository's own [rule modules](community-rules.md); a pattern containing `_` goes to the community linter, and every other one to the shipped linter.

Turning a default rule off hides every finding it would report in the repository, including future ones. For one site, prefer a [`//lint:ignore` directive](#suppressing-a-finding), which keeps the rule on everywhere else and records why.

## Suppressing a finding

Use Staticcheck's directive with a reason on the line above the finding, for example `//lint:ignore LV1001 external schema requires this field`, or `//lint:file-ignore <code> <reason>` for a whole file. Prefer a proper type or record literal when possible. Staticcheck applies the directive for every rule, upstream analyzers included: `nilerr` reads `//lint:ignore nilerr` itself upstream, and here it reports the finding anyway so the directive suppresses it rather than being reported as matching nothing. A directive that matches no finding on its line is reported as unused, so a suppression cannot outlive the code it excused.

A suppression is a permanent decision about one site. Findings a repository already had when it adopted the rules, and means to fix, belong in its [baseline](configuration.md#baseline) instead, which accepts them only until they are fixed.

Staticcheck lints a package with tests twice, without its tests and with them, and judges a directive in each build on its own. For most rules a finding in a non-test file is the same in both builds, but `recvcheck`, `unparam`, and `gochecksumtype` also see the test files, so some of their findings exist in one build only, and a directive for one would be reported as unused by the other build. For those three rules one build decides the findings and directives on the lines of non-test files, and the other build leaves those lines alone: `recvcheck` is decided by the build with tests, `unparam` and `gochecksumtype` by the build without them. A directive for them that matches nothing in the deciding build is still reported as unused, except under `-tests=false`, where a `recvcheck` directive in a package with tests is left unjudged. Staticcheck exempts its own `U1000` the same way. With `-show-ignored`, the other build's match for each such directive is listed as an ignored finding that names the deciding build.

Every rule skips generated Go files: nobody edits generated code for style. Upstream analyzers still run over them, so the facts they export stay correct, but their diagnostics there are dropped. In a package that imports `"C"`, the analyzers see cgo's rewrite of each file, which cgo marks generated; every rule judges such a file by the original it maps back to, so hand-written cgo files are checked and reported at their own paths, while cgo's own additions such as `_cgo_gotypes.go` count as generated.

## The modernize selection

`golang.org/x/tools` ships `modernize` as a suite of separately named analyzers, each replacing a hand-written construct with the newer language or library feature that says the same thing. Five of them are on:

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

A bare `go fix ./...` applies the whole suite, including the rewrites that are [off](rule-selection.md#modernize-analyzers-left-off), so name the rules.

## The go-critic selection

[go-critic](https://go-critic.com/overview.html) tags each of its checkers. The `diagnostic` tag marks likely bugs; `style`, `performance`, and `opinionated` are advice, and `experimental` marks checkers upstream has not settled. The linter runs the stable `diagnostic` checkers, minus four that repeat a rule already on, plus six experimental ones that nothing else covers. A seventh experimental checker, `deferInLoop`, is registered but [opt-in](#opt-in-resources-deferinloop). [Rule selection](rule-selection.md#go-critic-checkers-left-off) lists the checkers left off and why.

Each checker is registered as its own analyzer, so a finding's code is the checker's name, as with the modernize rules. `//lint:ignore offBy1 reason` silences one checker on one line, and `-checks='all,...,-offBy1'` turns one off; there is no single `gocritic` code or glob that covers them all.

| Checker | Reports |
| --- | --- |
| `appendAssign` | `x = append(y, ...)` where `y` is not `x`, usually a copy-paste slip |
| `argOrder` | Arguments that look swapped, such as `strings.HasPrefix("#", line)` |
| `badCall` | A call that does nothing useful, such as `strings.SplitN(s, sep, 0)` or `filepath.Join` of one element |
| `badCond` | A condition that is always true or false, or a loop condition that points the wrong way |
| `badRegexp` | A valid regular expression with a likely mistake, such as a repeated character in a class; experimental upstream |
| `badSyncOnceFunc` | `sync.OnceFunc(f)` whose result is discarded or called on the spot, so `f` never runs, or runs every time; experimental upstream |
| `codegenComment` | A generated-file comment tools do not recognize, so the file is linted and reviewed as hand-written |
| `deprecatedComment` | A deprecation notice not written as `Deprecated: `, which tools and pkg.go.dev then miss |
| `dupArg` | The same argument twice where that is a no-op, such as `copy(dst, dst)` |
| `dupBranchBody` | An `if` whose two branches are identical |
| `dupCase` | A `switch` case listed twice, which can never match the second time |
| `evalOrder` | `return x, f(&x)`, whose first result depends on an evaluation order the language leaves unspecified; experimental upstream |
| `exitAfterDefer` | `log.Fatal` or `os.Exit` in a function with deferred calls that will not run |
| `filepathJoin` | A path separator inside one `filepath.Join` element; experimental upstream, and it found two in Levenshtein's own tests |
| `flagDeref` | Dereferencing a `flag` pointer at definition, which reads the default instead of the parsed value |
| `flagName` | A flag name with whitespace that no command line can pass |
| `mapKey` | A map literal key with stray whitespace next to keys without it |
| `offBy1` | Indexing a slice at its length, which always panics |
| `rangeAppendAll` | `append(out, xs...)` inside a loop over `xs`, which appends the whole slice on every pass where one element was meant; experimental upstream |
| `returnAfterHttpError` | An `if` block that ends with `http.Error` and no `return`, so the handler writes its normal response after the error; experimental upstream |

`badCall` overlaps in part: `SA1018` also reports `strings.Replace` with a count of zero, and `SA4021` a single-argument `append`, so those two lines get a finding from each. It stays on because nothing else reports its `SplitN` and one-element `filepath.Join` cases.

## Known bug patterns

These analyzers are on because the pattern each reports is a known bug rather than a matter of style, its false alarms are rare, and its fix is local, even where they found nothing in the codebases measured ([why that is an exception](rule-selection.md#the-bar-for-a-rule)):

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
| `reflectvaluecompare` | Two `reflect.Value`s compared with `==` or `reflect.DeepEqual` compare the reflect package's own representation, so equal values can compare unequal. `go vet` ships the pass but leaves it out of its default suite |
| `httpmux` | Before Go 1.22, `ServeMux` treats `"GET /items/{id}"` as a literal path, so a module whose `go` directive predates 1.22 registers a route no request reaches. It reports nothing in a module on Go 1.22 or later |
| `gochecksumtype` | Adding a variant to an interface used as a closed set leaves every type switch that does not list it falling through to its `default`. It checks only interfaces marked `//sumtype:decl`, so it reports nothing in a module that marks none |
| `badSyncOnceFunc` | `sync.OnceFunc(f)` returns the function that runs `f` once; a statement that discards it never runs `f`, and `sync.OnceFunc(f)()` runs it every time |
| `evalOrder` | In `return x, f(&x)`, whether `x` is read before or after `f` changes it is unspecified, and the compiler's choice can change between releases |
| `rangeAppendAll` | `append(out, xs...)` inside `for _, x := range xs` appends the whole slice on every pass, a slip for `append(out, x)` |
| `returnAfterHttpError` | `http.Error` writes the response but does not stop the handler, so without a `return` the normal response is written after it |

## Upstream analyzer settings

These upstream analyzers need a word about scope or settings:

- `unparam` skips exported functions, its own default. The linter checks one package at a time, so it cannot see callers in other packages, and changing an exported signature would break them. Functions in a `main` package are checked either way, since nothing can import them. A function in a non-test file is judged by the package's own callers, in the build without tests: a parameter that receives the same value at four or more call sites in non-test code is reported even when a test passes other values, and a test's extra calls cannot make a parameter or result look unneeded. `//lint:ignore unparam <reason>` on the function suppresses such a finding like any other. Functions in test files are judged with the tests.
- `musttag` checks the calls it knows: `encoding/json`, `encoding/xml`, `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`, `github.com/mitchellh/mapstructure`, and `github.com/jmoiron/sqlx`. It skips named struct types declared outside the module that contains the package, found from the nearest `go.mod`, and types that implement the matching marshaler interface. A field tagged with the name it already had, such as `json:"Checksum"`, is the fix that keeps existing data readable. The analyzer skips an argument that is a bare variable name, because Staticcheck's loader parses without the object resolution it uses to tell a variable from `nil`, so `json.Marshal(value)` is not checked while `json.Marshal(&value)`, `json.Unmarshal(data, &value)`, and a composite literal are.
- `contextcheck` reports a function that has a context but calls something that starts its own, directly or through a chain of calls, so cancelling the caller does not stop the work. It follows those chains within one package only: Staticcheck's runner hands every package fact to its own analyzers regardless of type, and they panic on the facts contextcheck exports, so it runs with facts off. A call into another package that makes its own context is missed rather than misreported. Its known false alarms are work that is meant to outlive the caller, such as a cleanup after cancellation or a goroutine that finishes a request's side effects: derive that context with `context.WithoutCancel(ctx)`, which the check accepts and which keeps the caller's values, or use `//lint:ignore contextcheck reason` on the call. A function that returns a context is treated as a constructor and not reported, and an HTTP handler is treated as having the request's context.
- `recvcheck` keeps its built-in exclusions for `UnmarshalText`, `UnmarshalJSON`, `UnmarshalYAML`, `UnmarshalXML`, `UnmarshalBinary`, and `GobDecode`, which need a pointer receiver even on a type whose other methods take values. Methods declared in generated files do not count: code generators such as Dagger's add a value-receiver `MarshalJSON` to a type whose hand-written methods take pointers, and nobody can change the generated receiver. Methods declared in test files count, so a pointer-receiver helper in a test file makes a value-receiver type a mix; `//lint:ignore recvcheck <reason>` on the type suppresses it.
- `fatcontext` checks loops and function literals, its defaults. Its `check-struct-pointers` mode stays off: upstream marks it a potential finding, and it cannot tell a context stored in a struct for later use from one that grows.
- `usetesting` reports `os.Chdir`, `os.Setenv`, `os.MkdirTemp`, and `os.CreateTemp` with an empty directory inside a function that takes a `*testing.T`, `*testing.B`, or `testing.TB`, and `os.Chdir` only in packages whose `go` version has `t.Chdir` (1.24). `os.Setenv` is on here although upstream leaves it off, because a variable left set is the likeliest of the four to change another test's result. Its `context.Background`, `context.TODO`, and `os.TempDir` detections stay off: `t.Context()` is a better spelling, but a background context in a test changes nothing another test sees, and `os.TempDir` only names a directory.
- `bidichk` reports all nine bidirectional control characters, its default.
- `gocheckcompilerdirectives` checks a directive that has an argument after it, such as `//go:generate stringer` or `//go:linkname local remote`. A directive with nothing after it, such as a misspelled `//go:noinline`, is not checked.
- `zerologlint` follows an event through branches and into a function it is passed to, one call deep. A function that returns a `*zerolog.Event` for its caller to finish is reported, because nothing dispatches the event inside it; mark such a builder with `//lint:ignore zerologlint reason`.
- `loggercheck` checks logr, `k8s.io/klog/v2` (`InfoS`, `ErrorS`, and their variants), zap's sugared `With` and `...w` methods, and go-kit log, which is off upstream and on here because an odd key-value list is the same bug there. It skips a call that spreads a slice with `...`. `log/slog` is left to go vet's `slog` check, which reports the same missing value and a key that is not a string, so a slip in a `slog` call is one finding in `go-vet` rather than one in each check. A configuration that runs `go-lint` without `go-vet` gets no report of it. Its `requirestringkey` and `noprintflike` options stay off: a key held in a variable is fine, and a `%` in a message is not always a format verb. Its report that a nil pointer to a `fmt.Stringer` may panic is dropped: zap, klog, logr's `funcr`, and `fmt` all recover from a `String` method that panics on a nil receiver.
- `exptostd` suggests a replacement only when the module's `go` version has it: Go 1.21 for most of `slices` and `maps`, and Go 1.23 for `maps.Keys` and `maps.Values`, which return iterators in the standard library.
- `gochecksumtype` reports under golangci-lint's name for it; upstream's own analyzer name is `sumtype`. A sum type is an interface marked with a `//sumtype:decl` comment that has an unexported method, so no other package can add a variant. A `default` case does not satisfy it, the opposite of upstream's default and the same as `exhaustive` here: a switch that must handle a new variant should fail when one is added. It runs without facts, for the same reason as `contextcheck`, so it checks switches over sum types declared in the same package; a switch over a sum type imported from another package is not checked. A switch in a non-test file is judged without the tests, so a variant a test declares, such as a fake, does not make it incomplete; a switch in a test file must list it.
- `httpmux` reads the `go` directive of the module being linted, so a module on Go 1.22 or later gets no diagnostic, whatever the toolchain.
- `testableexamples` checks `Example` functions in `_test.go` files. An example that has nothing to compare can be kept with an `// Output:` comment and no expected text, which makes `go test` run it and fail only if it prints something.

## Errors and enum switches

Keep errcheck's upstream exclusions for operations documented never to fail. Intentionally ignored errors require explicit `_ =`, preferably with a reason; do not add broad Close/Write exclusions. Check write/flush/close errors when they affect persisted data. A default switch branch does not satisfy exhaustive; list all declared enum values, or use a narrow justified suppression for intentionally partial switches. These checks do not prove runtime enum validity or that an assigned error is handled.

## Opt-in complexity: gocognit

`gocognit` reports a function whose [cognitive complexity](https://github.com/uudashr/gocognit#cognitive-complexity) is over 30. Each `if`, loop, `switch`, `select`, jump, and run of mixed `&&`/`||` adds one, plus one more for each level of nesting it sits in, so the score tracks how much a reader has to hold in mind rather than how many paths a test needs. The binary registers it, and the shipped selection turns it off with `-gocognit`, because a threshold says a function is hard to maintain, not that it is wrong: it would fail builds on working code, which is the bar every default rule is held to.

The threshold is fixed at 30, golangci-lint's default and the common line past which a function is hard to maintain. A repository can turn the rule on, but the line itself is not a setting, so a consumer that wants a different one cannot set it. Levenshtein does not opt itself in: its policy analyzers and several table-driven tests are over the line. To see what the rule would ask of a repository, [run the linter directly](#running-the-linter-directly) with `-checks=gocognit`.

To opt in, add it to the repository's `go-lint` check, which [appends it to the shipped selection](#changing-the-selection-for-one-repository), after `-gocognit`, so it wins:

```json
"cleanup": {"kind": "go-lint", "target": "app", "environment": "go", "lint": {"checks": ["gocognit"]}}
```

The check then fails on every function over the line, so fix or suppress the existing ones in the same change, or record them in a [baseline](configuration.md#baseline). A finding can be suppressed like any other, with `//lint:ignore gocognit <reason>` above the function.

## Opt-in resources: deferInLoop

go-critic's `deferInLoop` reports a `defer` inside a loop. A deferred call runs when the function returns, not when the loop's pass ends, so a loop that opens a file, a response body, or a lock on each pass and defers its release holds every one of them until the loop is done. Over a long or unbounded loop that exhausts file descriptors or connections. The usual fix moves the body of the loop into a function of its own, so each pass's `defer` runs at the end of that pass.

It is registered and off by default, with `-deferInLoop` in the shipped selection, because most of what it finds is harmless: across the [codebases measured](rule-selection.md#measured-on-other-codebases), one of its 34 findings held resources long enough to matter, and the rest were tests or short loops over a fixed few items. A repository whose loops handle requests, files, or rows in bulk can turn it on the same way as `gocognit`:

```json
"cleanup": {"kind": "go-lint", "target": "app", "environment": "go", "lint": {"checks": ["deferInLoop"]}}
```

Suppress a loop that is known to be short with `//lint:ignore deferInLoop <reason>` on the `defer` line.

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

For defined string types with package-level typed constants, the check also rejects nonempty string literals and constant expressions used as values, including comparisons, struct literals, assignments, calls, and returns. Declare spellings in constants. Empty zero values and conversions from runtime input remain allowed; this is not runtime enum validation. Only types declared in the module being linted count, judged by import path: a library type such as Kubernetes's `corev1.ResourceName` declares a few well-known constants but accepts any name, so `corev1.ResourceName("nvidia.com/gpu")` is allowed. A switch or comparison chain over such a type is judged like one over a plain string.

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

The analyzer tracks new local values made with a literal, `&T{}`, `new(T)`, or a zero-valued `var`. It reports at the creation/declaration when fields are assigned before the value is first used, including nested value fields and construction in `if` branches and in `switch`, type switch, and `select` cases. One diagnostic covers a construction sequence.

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

It also checks the Go files in a package's directory that the build leaves out, as `gofmt -l` would: files for another platform such as `foo_windows.go`, and files behind a build tag such as `//go:build integration` or `//go:build ignore`, test files included. Excluded files are not type-checked, so the other rules do not see them. A directory with no package the default build keeps, such as one holding only a `//go:build ignore` generator, is not checked.

Excluded files are checked after Staticcheck's run, outside its cache, which keys a package's results on the files its build compiles. The linter lists them with one `go list` of the linted patterns under the run's `-tags` and checks each one on every run, so formatting an excluded file clears its finding on the next run, and an edit to one never re-lints a package. Their findings follow `-checks`, each directory's `staticcheck.conf` under `-checks=inherit`, `-fail`, `-tests=false` (which leaves excluded test files alone), and a `//lint:file-ignore LV1005 <reason>` in the file, and read exactly as Staticcheck prints LV1005's own, at line 1, column 1. Only the `text` and `json` output formats, which print one line per finding, carry them: a run with `-f stylish`, `sarif`, `binary`, or `null`, or with `-matrix`, leaves excluded files unchecked. `./verify go-lint` and the Dagger runner use `json`.

## Tests that can fail: LV1006

LV1006 reports a test that passes whatever the code under test does. It looks at each `TestXxx(t *testing.T)` in a `_test.go` file, including one that names `testing.T` through an alias such as `type T = testing.T`, and reports two shapes:

- **No assertion.** Nothing in the body, including subtest literals and cleanup callbacks, can fail the test. A test value counts as a way to fail when it is used as anything other than the receiver of a method that cannot report a failure (`Log`, `Parallel`, `Helper`, `Cleanup`, `TempDir`, `Setenv`, `Skip`, `Failed`, and similar). So `t.Errorf`, `t.Fatal`, `require.Equal(t, ...)`, a helper that receives `t`, a struct that stores `t`, and `t.Run` with a named function all count. An explicit `panic`, `log.Fatal`, `log.Panic`, `os.Exit`, or `runtime.Goexit` counts too. A panic or exit inside code the test calls, such as `regexp.MustCompile`, a third-party logger's `Fatal`, or a local wrapper around `os.Exit`, does not: state what the test expects with an assertion.
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

LV1006 only proves that a test can fail. It does not prove the test fails when behavior is wrong. The pinned upstream checks catch assertions that compare a value with itself or a constant with a constant: Staticcheck's `SA4000` for identical operands, and testifylint's `useless-assert` for calls such as `assert.Equal(t, x, x)`. The [`go-mutation`](mutation.md) check goes further and fails when a test does not catch a deliberate change to the code it covers.

## Running the linter directly

The linter is an ordinary Go command in its own module, `runner/lint`, so it runs without a checkout, Dagger, or `levenshtein.json`. From the root of the module to lint:

```sh
go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@latest ./...
```

Without `-checks` it runs the shipped selection, `all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022,-gocognit,-deferInLoop`, the same list `runner/toolchain.json` gives every `go-lint` check. `-checks` replaces that list. To reproduce a repository's `go-lint` check, pass the shipped list followed by the patterns the check adds, such as `,gocognit` to [opt in to gocognit](#opt-in-complexity-gocognit) or `,deferInLoop` to [opt in to deferInLoop](#opt-in-resources-deferinloop). `-checks=inherit` defers to a `staticcheck.conf` instead. The other Staticcheck flags work as usual: `-f=json` or `-f=sarif` for machine-readable output, `-list-checks`, and `-explain CODE`.

The module needs Go 1.27.1 or later; `go` 1.21 or later downloads that toolchain itself as long as `GOTOOLCHAIN` allows it, which is the default. A direct run is the `go-lint` rules and nothing else: no `go vet`, baseline, community rules, result cache, or text-format hints, which `./verify` adds. Its Staticcheck analysis cache is Staticcheck's own, under the user cache directory.

`@latest` resolves to the newest `runner/lint/vX.Y.Z` tag, or to the newest commit on `main` while there is none. To pin one revision, name a commit (`@<sha>`) or a `runner/lint/vX.Y.Z` tag, which [a release](maintainers/releases.md#publishing-a-release) creates beside `vX.Y.Z`; `go install` with the same argument keeps a binary on `PATH`. From a checkout, `(cd runner/lint && go build -o /tmp/levenshtein-lint ./cmd/levenshtein-lint)` builds the same command.

## How the rules are built

The analyzers use Go's `go/analysis` framework and Staticcheck's runner for package loading, caching, diagnostics, and suppression. Every rule skips generated Go files, the house rules on their own and the upstream analyzers through a shared wrapper that also restores each analyzer's name as the reported code. Analyzer regression fixtures live in `runner/lint/policy/testdata` and run with `cd runner/lint && go test ./...`. Staticcheck keeps its own analysis cache, and the default selection changes only when a pinned analyzer version does; review new findings with dependency upgrades.
