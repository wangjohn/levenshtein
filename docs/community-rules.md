# Community lint rules

**Status: proposal.** Nothing on this page is shipped behavior except the
example module and the CI check that builds it.

This page proposes how people outside this repository can publish Go lint rules
that Levenshtein consumers turn on, without those rules entering `runner/lint`,
and how a catalog of those rules can grow over time. The
[roadmap](roadmap.md#what-would-justify-expansion) defers public plugins until
they have "actual external consumers", and the [phasing](#phasing) keeps to that
gate.

## Summary

- **Authors** publish an ordinary Go module whose `lvrules` package declares a
  `Namespace` and returns `[]*analysis.Analyzer`. It does not import Levenshtein.
  The rules also work in golangci-lint, nogo, and `singlechecker`.
- **Consumers** pin the module by exact version in `levenshtein.json` and say
  which rules to run:

  ```json
  "rules": {
    "github.com/acme/lvrules-errors": {"version": "v1.4.0", "select": ["acme_*"]}
  }
  ```

- **Levenshtein** builds the selected modules into a second linter binary on the
  same pinned Staticcheck. It runs that binary next to the core linter, without
  network access, and merges both sets of findings into one report. Each
  community finding carries its source module and documentation link.
- **The catalog**, in its own repository, lists modules that build with the
  current release, pass their tests, and have an owner. Rules that prove
  themselves there can graduate into the core selection.

## Where things stand

Every rule a `go-lint` check can report is compiled into one binary,
`levenshtein-lint`, which `runner/lint/cmd/levenshtein-lint/main.go` assembles
from Staticcheck, upstream `go/analysis` analyzers, and the house `LV*` rules in
`runner/lint/policy`. The runner builds it inside Dagger from the pinned shared
checkout. A consumer's `lint.checks` can only switch registered rules on or
off. Today the only way to run a rule Levenshtein does not ship is a native
`command` check, which gives up the shared selection syntax, `//lint:ignore`,
JSON findings, and Dagger result caching.

## What other projects do

| Project | How rules are added | Pinning and trust | Discovery |
| --- | --- | --- | --- |
| [golangci-lint module plugins](https://golangci-lint.run/docs/plugins/module-plugins/) | `.custom-gcl.yml` lists Go modules; `golangci-lint custom` rebuilds the binary with them compiled in. Plugins call `register.Plugin` and return `[]*analysis.Analyzer` | Module version, verified by `go.sum` and the checksum database | Its [linters list](https://golangci-lint.run/docs/linters/) covers bundled linters only; there is no plugin index |
| golangci-lint `.so` plugins | Go `plugin` package, loaded at run time | Must match the host's Go version, platform, and every shared dependency exactly | Deprecated in favor of module plugins |
| [TFLint](https://github.com/terraform-linters/tflint/blob/master/docs/user-guide/plugins.md) | Separate binaries speaking gRPC through `go-plugin` | Exact `version` only, no ranges; GitHub artifact attestations or PGP signatures | `tflint-ruleset-*` naming and a template repository |
| [ESLint](https://eslint.org/docs/latest/extend/plugins) | npm packages loaded in process; rules addressed as `plugin/rule` | npm lockfile; `eslint` as a peer dependency | `eslint-plugin-*` naming, npm keywords, awesome-eslint |
| [Semgrep Registry](https://github.com/semgrep/semgrep-rules) | Declarative YAML rules | Reviewed PRs with a CLA and a rule quality check in CI | Hosted registry; every rule needs `# ruleid:` and `# ok:` test cases |
| [pre-commit](https://pre-commit.com/) | Hook repositories that run as subprocesses | `rev` must be an immutable tag or SHA; `autoupdate --freeze` | A hooks page curated by PR |
| [Vale](https://vale.sh/docs/keys/packages) | Declarative style packages, `vale sync` | Versioned release archive URLs | Package explorer built from a library index |
| [nogo](https://github.com/bazel-contrib/rules_go/blob/master/go/nogo.rst) | Analyzers compiled into the build | Bazel module versions | None |
| [Ruff](https://docs.astral.sh/ruff/faq/) | No plugins by design; popular flake8 plugins are reimplemented under new prefixes | Not applicable | Not applicable |

Five patterns recur:

1. **One rule interface the ecosystem already uses.** For Go that interface is
   `*analysis.Analyzer`. golangci-lint, nogo, gopls, `go vet -vettool`, and
   Staticcheck's `lintcmd` all accept it, so a rule author writes it once.
2. **Build from source instead of loading binaries.** Go's `.so` plugins broke
   whenever the toolchain or a shared dependency changed, and golangci-lint
   replaced them with rebuilds. Dylint rebuilds against the exact Rust
   toolchain for the same reason.
3. **Exact, immutable pins.** TFLint rejects version ranges, and pre-commit
   caches by `rev` on the assumption that it never moves.
4. **Rule IDs owned by the plugin.** ESLint's `plugin/rule`, RuboCop's
   `Department/Cop`, and Vale's `Style.Rule` mean the same thing in every
   repository, so documentation, ignore comments, and answers online all carry
   over.
5. **Discovery that grows in steps.** First a naming convention, then a catalog
   curated by PR, and only later a hosted explorer. The catalog is data in
   git, so adding a rule is just a pull request.

## Rule modules

A rule module is any Go module with an `lvrules` package that exports two
things:

```go
package lvrules

import "golang.org/x/tools/go/analysis"

// Namespace prefixes every rule this module contributes.
const Namespace = "acme"

// Analyzers returns the rules this module contributes.
func Analyzers() []*analysis.Analyzer
```

The module does not import Levenshtein. The protocol is `go/analysis`, which
Levenshtein does not own or version, so there is no Levenshtein SDK to keep
compatible.

Each analyzer must meet these requirements. The build rejects a module that
breaks one of the first three, naming the module and rule (see
[errors](#errors)); catalog admission checks the fourth:

- **`Name`** is a Go identifier that `analysis.Validate` accepts and is unique
  within the module, ignoring case.
- **`Doc`** starts with a one-line summary. `-list-checks` and the catalog
  show that line.
- **`URL`** links to the rule's documentation: what it reports, why, how to fix
  it, and how to suppress it. Link to a stable location, such as pkg.go.dev or
  a docs page for each release.
- **Not a copy of a core rule.** A module that returns `nilness.Analyzer`, for
  example, reports every finding twice (see [admission](#admission)).

The module must also build with the Go version in `.go-version` and the
Staticcheck version in `runner/toolchain.json`. It may require newer versions
of other shared dependencies, such as `golang.org/x/tools`, as long as
Staticcheck still builds with them. Community rules run in their own process,
so a newer `x/tools` never reaches the core rules.

Rules may use analysis facts and suggested fixes. Levenshtein reports findings
and does not apply fixes; the module's own `singlechecker` wrapper applies them
with `-fix`.

## Rule names

A rule reports as `<namespace>_<name>`, such as `acme_nopanic`. That one name
appears in findings, in `select` and `lint.checks` patterns, and in
`//lint:ignore acme_nopanic <reason>`.

- **The module owns the namespace.** It is part of the module's published
  interface, like an ESLint plugin name, so `acme_nopanic` means the same rule
  in every repository. The catalog records each namespace against one module
  and refuses a second claim.
- **Lowercase letters only.** Staticcheck reads a pattern of letters followed
  by `*` as a category: the part of a code before its first digit, compared
  ignoring case. A namespace with a digit would join a core category: namespace
  `sa1` would make `SA1*` select `sa1_nopanic`. With letters only, and a `_`
  that no core rule uses, a community code can never match a core pattern.
- **`_`, not `/`.** Analyzer names must be Go identifiers.
- **An alias only resolves a collision.** Two uncatalogued modules can claim the
  same namespace. A consumer then sets `"alias"` on one of them, and every
  finding from that module notes the alias. Aliases are the exception, because
  they break the shared meaning of a name.
- **Renaming is a breaking change.** Renaming a namespace or rule needs a new
  major version, and see [renames](#renames-deprecation-and-graduation).

## Consumer configuration

A new optional top-level `rules` object maps a module path to its settings:

```json
"rules": {
  "github.com/acme/lvrules-errors": {
    "version": "v1.4.0",
    "select": ["acme_*", "-acme_wrapf"],
    "advisory": ["acme_sentinel"],
    "settings": {"acme_nopanic": {"allow": "Must,Should"}}
  }
}
```

| Field | Meaning |
| --- | --- |
| `version` | Required. A semantic version tag or Go pseudo-version. Branch names and `latest` are configuration errors |
| `select` | Required. Patterns, in the `lint.checks` syntax, for the rules every `go-lint` check runs from this module. The module's own namespace is the only one they may name. An empty list is an error; to stop using a module, remove it |
| `advisory` | Rules whose findings appear in the report but do not fail the check, so a team can trial a rule. Only `semantic-lint` is advisory today |
| `settings` | Flag values for each rule, set through the analyzer's `Flags`, the way `runner/lint` configures `gocognit` and `usetesting`. An unknown rule or flag is a configuration error |
| `alias` | Replaces the module's namespace when two modules collide |

`rules` is an additive, optional field of version 1, like `lint`, so a file
without it keeps its meaning and cached results.

### Selection

- **One place to switch rules on.** `select` applies to every `go-lint` check,
  so adopting a module takes one edit. A check's `lint.checks` can still add or
  remove `acme_...` patterns for that check only.
- **`all` and `*` mean core rules.** They select nothing from community modules.
  Adding a module never changes what an existing `all` selects, and upgrading
  Levenshtein never turns on a community rule.
- **Globs.** `acme_*` selects the whole namespace and `acme_no*` every rule
  starting with that prefix: a pattern containing `_` and ending in `*` is a
  literal prefix. The runner expands these patterns into exact names, because
  Staticcheck's category rule would otherwise match nothing. The pattern
  expression in `runner/main.go` and `internal/verify/validation.go` (today
  `^-?(\*|[A-Za-z][A-Za-z0-9]*\*?)$`) must accept `_` in both copies, and
  `allowed`/`selects` gain the prefix case.
- **Unselected rules do not run.** Staticcheck runs every registered analyzer
  and only then filters findings. The runner therefore builds each module's
  rules into the community linter only when `select` names one of them.

### Executors

In phase 1, `rules` needs a check on the Dagger executor. A native `go-lint`
check in a configuration with `rules` is a configuration error: "community
rules need the dagger executor; github.com/acme/lvrules-errors is declared but
check native-go-lint runs natively". Native support is planned for phase 3,
under the same trust rules as native `command` checks (see
[security model](#security-model)).

## Running community rules

Community rules run in a second binary, `levenshtein-community-lint`, never
inside `levenshtein-lint`. A prototype confirmed these reasons:

| In one binary | In a separate binary |
| --- | --- |
| A community analyzer that exports a package fact crashed the whole linter with `interface conversion: analysis.Fact is *levenshtein.pkgFact, not *deprecated.IsDeprecated`, because Staticcheck's runner hands every package fact to its own `deprecated` analyzer. This happened even with the rule deselected | The same analyzer ran cleanly |
| A module could raise `golang.org/x/tools` under the core rules through minimum version selection and silently change core findings. Preventing that meant authors could never require a newer dependency than the pinned linter | Only Staticcheck must stay at its pin. Other shared dependencies may move, and they only affect community rules |
| A community rule that crashes, writes to stdout, or calls `os.Exit(0)` takes core findings with it; `os.Exit(0)` makes the check pass | Core findings are unaffected; the community half fails with an error naming its modules |
| One package load | A second package load. On this repository's root module (4 CPUs, cold Staticcheck cache), the core linter took about 6.5 s and a community linter with two rules about 2.5 s, almost all of it loading. Checks without community rules pay nothing |

`//lint:ignore` works across the two processes: Staticcheck reports an unused
ignore directive only when the directive names a selected rule. The core linter
never flags `//lint:ignore acme_nopanic`, and the community linter never flags
`//lint:ignore SA4006`. A stale ignore for a community rule is still reported,
by the community linter.

### Findings

The runner merges both processes' JSON into the check's findings and adds two
fields to every community finding:

```json
{
  "code": "acme_nopanic",
  "message": "Load panics; return an error so callers can handle the failure",
  "location": {"file": "store/store.go", "line": 7, "column": 3},
  "source": "github.com/acme/lvrules-errors@v1.4.0",
  "url": "https://pkg.go.dev/github.com/acme/lvrules-errors/nopanic",
  "advisory": false
}
```

Core findings should gain `url` as well: Staticcheck's own rules have pages at
`https://staticcheck.dev/docs/checks#<code>`, and the rest can link to their
row in [shared checks](checks.md#go-lint-rules). `-explain` cannot help here,
because `lintcmd` prints that staticcheck.dev link for every rule, community
rules included. The generated registration therefore makes `-list-checks` show
each rule's `Doc` summary instead of a blank title, and the `url` field is where
the documentation link lives.

### Errors

Problems are reported where they can be fixed, never as a Go stack trace from
generated code:

- **Configuration errors (exit 2)** name the module and what to change: a bad
  version, an unknown rule in `select` or `settings`, a namespace that is not
  lowercase letters, a missing `URL`, duplicate rule names, or a module that
  needs a newer Go or Staticcheck than this release. For example: "github.com/acme/lvrules-errors@v1.5.0
  requires honnef.co/go/tools v0.9.0, but this Levenshtein release pins v0.8.1;
  pin v1.4.0 or upgrade Levenshtein".
- **Rule failures** become findings. A rule's `Run` is wrapped so that a
  returned error or a panic is reported at the package clause as
  "`acme_nopanic` failed: ...". Staticcheck's runner otherwise records analyzer
  errors and exits 0. A prototype analyzer that returned an error produced no
  output at all, which is a silent pass. The same fix belongs in the core
  linter.
- **A crash of the community linter** fails the check as an error naming its
  modules, and the core findings are still reported.

## Build

For a `go-lint` check whose configuration selects community rules, the `GoLint`
Dagger function gains a `rules` argument and:

1. **Resolves in a build container.** It generates a module that requires the
   pinned Staticcheck and each selected `module@version`, runs `go mod tidy`
   with `GOPROXY` and `GOSUMDB` on, and builds `levenshtein-community-lint`.
   Content that differs from what the checksum database recorded fails the
   build.
2. **Checks the pins.** If resolution moved `honnef.co/go/tools` off its pin, or
   a module's `go` directive is newer than `.go-version`, the build fails with
   the configuration error above. `GOTOOLCHAIN=local` stops Go from switching
   toolchains instead.
3. **Warns on retractions.** `go list -m -retracted` shows whether a pinned
   version was retracted by its author, and the check reports that as a
   warning.
4. **Copies only the binary** with `WithFile` into a fresh image to run. See
   [security model](#security-model).

The generated registration uses positional import aliases, so a namespace can
never collide with an identifier in the generated file:

```go
// Code generated by levenshtein from levenshtein.json; DO NOT EDIT.

package main

import (
	m0 "github.com/acme/lvrules-errors/lvrules"
	"github.com/wangjohn/levenshtein/runner/community"
)

var modules = []community.Module{
	{Path: "github.com/acme/lvrules-errors", Version: "v1.4.0", Namespace: m0.Namespace, Analyzers: m0.Analyzers()},
}
```

`runner/community` is a new module holding the validation, renaming, and `Run`
wrapper, and `main.go` there hands the result to `lintcmd`. It has the same
Staticcheck pin as `runner/lint` and no other connection to it.

### Result keys

The CLI computes result keys before any executor runs, so they cannot include
what `go mod tidy` resolves inside Dagger. The key includes the declared
`rules` entries (module, version, `select`, `advisory`, `settings`, `alias`) and
the shared implementation, as it already does for `lint.checks`. Given the
pinned Staticcheck, the checksum database, and minimum version selection, those
inputs determine the resolved graph. Changing any of them re-runs the check,
and Dagger caches the community linter build under the same inputs.

## Security model

A community rule is code execution with read access to your source. A
malicious rule could send a private repository's code somewhere else. The
design limits that risk, but it cannot remove it by review or by pinning:

- **No network while rules run.** Only the build step fetches modules. The
  lint step runs with Dagger networking disabled.
- **Nothing shared is writable.** The lint step does not mount the Go module
  cache, the build cache, or the core linter's Staticcheck cache volume. It
  uses its own Staticcheck cache volume, keyed by the resolved module set, so a
  rule cannot poison results for other checks or other consumers on the same
  engine.
- **No credentials in the run image.** Anything the build needs, now or later,
  stays in the build container. Only the binary is copied out.
- **Pins that cannot move.** An exact version checked against the checksum
  database means an upstream push or a replaced tag cannot change what runs.
- **Core findings stay honest.** A misbehaving rule cannot hide or rewrite core
  findings, which come from another process.
- **Native execution is trusted-only.** When phase 3 adds native support,
  community rules run on the host with the same access as the user. `SECURITY.md`
  gets a section alongside "Not a sandbox" for `command` checks.

`SECURITY.md` should also say that vulnerabilities in a community rule go to
that module's maintainers, and a malicious module to the catalog maintainers
(see [governance](#governance)).

### Private modules

Private modules are out of scope for phase 1: the build has no way to receive
credentials, and the checksum database cannot verify a private module. A later
design would pass a token as a Dagger secret to the build container only, set
`GOPRIVATE` for the module's path, and require a `sum` field (the `h1:` hash)
in the `rules` entry, so the pin still verifies content.

## Upgrades

An exact pin in `levenshtein.json` is invisible to Dependabot and Renovate
unless they are told where to look. Ship a Renovate preset with a regex manager
for `rules` entries, and document it next to the Levenshtein pin update docs:

```json
{
  "customManagers": [{
    "customType": "regex",
    "managerFilePatterns": ["/(^|/)levenshtein\\.json$/"],
    "matchStrings": ["\"(?<depName>[^\"]+)\":\\s*\\{\\s*\"version\":\\s*\"(?<currentValue>v[^\"]+)\""],
    "datasourceTemplate": "go"
  }]
}
```

Rule authors publish GitHub releases with notes, which Renovate links in its
pull requests. A later `./verify rules` command can list declared modules, their
pins, newer versions, retractions, and catalog status.

## Renames, deprecation, and graduation

Rule names end up in ignore comments across many repositories, so a rename
must not turn into a wave of "this linter directive didn't match anything"
findings:

- **In a module.** `lvrules` may export `Renamed map[string]string`, which maps
  an old rule name to its new one. The runner accepts old names in `select`
  and `lint.checks` with a warning. The community linter reports each
  `//lint:ignore` that still names an old rule with a precise message:
  "`acme_panics` is now `acme_nopanic`; update this directive".
- **Into core.** When a community rule graduates, core keeps accepting its old
  code in selections and ignore directives for two minor releases, with the
  same kind of message pointing at the new `LV` code.
- **Deprecation.** A rule that `Doc` marks with a `Deprecated:` paragraph gets
  a warning on every check that selects it.

## Writing a rule module

[`examples/rule-module`](../examples/rule-module) is a complete rule module,
kept outside every Levenshtein module and build. Its namespace is `example`
and it contributes one rule, `example_nopanic`, which reports the builtin
`panic` in library code.

### Your first rule

1. **Start from the template.** Copy `examples/rule-module` (the
   `lvrules-template` repository in phase 0). Change the module path, choose a
   namespace, and check the catalog to see whether someone has claimed it.
2. **Write the rule** as an `analysis.Analyzer` with `Name`, `Doc`, and `URL`,
   and add it to `Analyzers()`.
3. **Write fixtures** under `testdata/src` with a `// want` comment on every
   expected finding, and clean files for the cases the rule must not report.
   Check that the test fails when you break the rule.
4. **Build it against Levenshtein.** Run the template's copy of
   `scripts/test-example-rules`, which builds the module into a community
   linter on the pinned Staticcheck and runs it on a sample package.
5. **Release.** Tag `v0.1.0`, write release notes, add the
   `levenshtein-lint-rules` topic to the repository, and name it
   `lvrules-<topic>`.
6. **List it.** Open a pull request to the catalog with the module's entry.

### The layout

```text
go.mod                         module github.com/wangjohn/levenshtein/examples/rule-module
LICENSE                        the catalog requires an OSI license
lvrules/lvrules.go             Namespace and Analyzers(), the only package Levenshtein imports
nopanic/nopanic.go             the rule, an ordinary analysis.Analyzer
nopanic/nopanic_test.go        analysistest over the fixtures below
nopanic/testdata/src/...       library (findings), app and shadowed (no findings)
cmd/nopanic/main.go            singlechecker, for running the rule on its own and applying fixes
```

### The rule

```go
var Analyzer = &analysis.Analyzer{
	Name:     "nopanic",
	Doc:      "return an error from library code instead of calling panic",
	URL:      "https://pkg.go.dev/github.com/wangjohn/levenshtein/examples/rule-module/nopanic",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}
```

`run` visits every call and reports the ones that type-check as the builtin
`panic`, so a local function named `panic` is not reported. It then skips
package `main`, `_test.go` files, `init` functions, and `Must` functions. A
panic inside a function literal is attributed to the declared function around
it, and a panic in package-level code is reported as such:

```go
func builtinPanic(pass *analysis.Pass, call *ast.CallExpr) bool {
	ident, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}

	builtin, ok := pass.TypesInfo.Uses[ident].(*types.Builtin)
	return ok && builtin.Name() == "panic"
}
```

### The entry point

```go
// Namespace prefixes every rule this module contributes, so nopanic reports as
// example_nopanic.
const Namespace = "example"

func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		nopanic.Analyzer,
	}
}
```

### Tests

`analysistest` fails on a missing finding and on any finding without a
`// want` comment:

```go
func Mustard(input string) int {
	if input == "" {
		panic("not a Must function") // want `Mustard panics; return an error so callers can handle the failure`
	}
	return len(input)
}
```

The fixtures cover `Must` and `MustParse` versus `Mustard`, `init` versus a
method named `init`, closures, package-level function literals, `_test.go`
files in library and `main_test` packages, and a shadowed `panic`. Each
exemption was checked by removing it and watching the test fail.

### Running it today

Before phase 1, a consumer can run the rule as a native `command` check through
its `singlechecker` wrapper. That gives up merged findings, selection, and
`//lint:ignore`:

```json
"nopanic": {"kind": "command", "target": "app", "environment": "host", "command": {"args": ["go", "run", "github.com/acme/lvrules-errors/cmd/nopanic@v1.4.0", "./..."], "rerun_args": ["go", "run", "github.com/acme/lvrules-errors/cmd/nopanic@v1.4.0", "./..."]}}
```

A module inside another repository, like the example, is tagged with its
directory as a prefix: `examples/rule-module/v0.1.0`.

### How this repository keeps the example working

The example is part of Levenshtein's own CI, so the template cannot rot while
the core toolchain moves:

- `levenshtein.json` declares it as the `example` target, so `go-lint`,
  `go-vet`, and `go-mod` on both executors, and `native-go-test`, cover it like
  the other modules.
- The `tests` job in `.github/workflows/verify.yml` runs
  `scripts/test-example-rules`, which:
  - runs the module's tests with `-race`;
  - builds a community linter from it on the Staticcheck version
    `runner/toolchain.json` pins, failing if the module would move that pin;
  - runs the linter on a sample package, expecting exactly one
    `example_nopanic` finding and a working `//lint:ignore example_nopanic`.

Dependabot watches `runner/lint` but not the example. The example should
require the lowest versions it needs, so the core can move ahead without
breaking it.

## The catalog

The catalog lives in its own repository, `levenshtein-lint-rules`, so community
rules never pass through this repository's review queue or release cadence.
It is run like any other open source project, with its own `README`,
`CONTRIBUTING`, `GOVERNANCE`, `SECURITY.md`, `CODE_OF_CONDUCT`, and
`CODEOWNERS`.

### Entries

One `catalog.json` lists every module:

```json
{
  "namespace": "acme",
  "module": "github.com/acme/lvrules-errors",
  "description": "Error handling rules for library packages",
  "license": "MIT",
  "maintainers": ["@acme-dev"],
  "rules": [
    {"name": "acme_nopanic", "summary": "return an error from library code instead of calling panic", "url": "https://pkg.go.dev/github.com/acme/lvrules-errors/nopanic"}
  ],
  "verified": {"version": "v1.4.0", "levenshtein": "v0.3.0", "at": "2026-10-01"},
  "status": "active"
}
```

`status` is `active`, `deprecated` (with a replacement), or `withdrawn`. The
runner warns when a consumer pins a module the catalog lists as `deprecated`,
and fails when it is `withdrawn`, which is reserved for malicious or dangerous
modules.

### Admission

A pull request that adds or updates an entry runs catalog CI, which:

- resolves the module and runs its tests;
- builds it into a community linter against the current Levenshtein release,
  checking the Go and Staticcheck pins;
- checks that the module's `Namespace` matches the entry and that the entry is
  the only one with that namespace;
- requires `Doc`, `URL`, at least one finding and one clean case in the tests,
  and an OSI license. Semgrep's registry shows why the license matters: its
  rules ship under the
  [Semgrep Rules License](https://github.com/semgrep/semgrep-rules/blob/develop/LICENSE),
  which limits reuse;
- runs each rule over a fixed corpus, starting with this repository and
  `runner/testdata/good`, and records the number of findings. `docs/checks.md`
  measures every upstream rule this way before turning it on;
- flags a rule whose findings land on the same lines as a core rule's on the
  corpus, which is how `docs/checks.md` found go-critic checkers that repeat
  Staticcheck.

A maintainer reviews every new namespace. Updates to an existing entry by its
listed maintainers merge once CI passes.

### What readers see

A static page generated from `catalog.json` shows for each module:

- its rules, with summaries and links;
- the latest version that built, labeled "builds with Levenshtein v0.3.0".
  The label is deliberately not "verified": the catalog checks that a module
  builds and passes its tests, not that its code has been audited;
- the date of its last release and last successful check;
- its corpus finding counts;
- how many public repositories reference it, counted through code search.
  Levenshtein collects no telemetry.

A scheduled job re-checks every entry against each new Levenshtein release and
marks the ones that stop building, so readers can tell a quiet module from a
broken one.

### Governance

`GOVERNANCE` in the catalog repository sets:

- **Namespaces.** First come, first served, for a module that exists and
  builds. Reserving a namespace for a module that doesn't exist yet counts as
  squatting and is refused, following crates.io's policy. Names that
  impersonate another project, or claim "levenshtein" or "lv", are refused.
- **Transfers.** A namespace moves to a new module only with the current
  maintainers' agreement, or after 12 months without a response on an issue
  asking for it, when the module no longer builds.
- **Removal.** A module that stops building for two consecutive Levenshtein
  releases is marked `deprecated`. A module that is malicious, or that exfiltrates
  or damages source, is marked `withdrawn` at once, and the catalog publishes an
  advisory.
- **Security reports.** A vulnerability in a rule goes to the module's own
  security contact. A malicious module goes to the catalog's `SECURITY.md`.

### Discovery

- The generated catalog page, with search.
- The GitHub topic `levenshtein-lint-rules`. The project name alone is buried
  under string-distance libraries.
- The repository naming convention `lvrules-<topic>`, matching the `lvrules`
  package.

### Graduation into core

The catalog also feeds the core selection. When a community rule has users in
several unrelated repositories, clear fixtures, and no overlap with a shipped
rule, propose it for `runner/lint` using the existing standard: a recurring
defect, known-bad and legitimate fixtures, repair guidance, and measured
cost. [Renames](#renames-deprecation-and-graduation) covers how existing
selections and ignore directives carry over.

## Alternatives considered

| Option | Why it is not the recommendation |
| --- | --- |
| Compile community rules into `levenshtein-lint` (golangci-lint's model, and this page's first draft) | The prototype crashed on a rule that exports a package fact, even with the rule deselected; a rule can hide core findings; and pinning shared dependencies to the core versions made authors' dependencies hostage to Levenshtein releases |
| Go `.so` plugins | The version skew that led golangci-lint to deprecate them; no Windows support |
| One vet tool per module (`go vet -vettool`, `unitchecker`) | Isolates each module, but every module loads the program again, findings leave `lintcmd`'s JSON, and `//lint:ignore` stops applying. The chosen design keeps one extra load for all community rules together |
| RPC plugins, as in TFLint | Needs a protocol designed and versioned here for something `go/analysis` already defines |
| Declarative rules only (Semgrep, ruleguard) | Lower risk and easy to review, but cannot express type-aware rules like `LV1001` or `LV1006`. They could be a second tier later: `go-ruleguard` bundles are also Go modules |
| A catalog in this repository | Puts every community rule in the main repository's review queue, which is what this proposal exists to avoid |

## Phasing

| Phase | Work | Gate |
| --- | --- | --- |
| 0 | Publish the `lvrules-template` repository from `examples/rule-module`, and document the rule module contract and the `command` check as the interim way to run rules | A public template is a contract. Changing the `lvrules` shape after phase 0 means migrating every published module, so settle this page first |
| 1 | `rules` config, the community linter on Dagger, selection and globs, merged findings with `source` and `url`, rule failures as findings, and a network-less lint step | Someone outside this repository and its pilots asks to run a rule Levenshtein will not ship |
| 2 | Catalog repository with governance, admission CI, scheduled re-checks, `status`, the Renovate preset, and renames | A second, unrelated rule module exists |
| 3 | Generated catalog page, corpus finding counts, native executor support, private modules | The catalog lists enough entries that browsing `catalog.json` is inconvenient, or a consumer needs native or private modules |

## Design review

Two independent reviews, one of product and user experience and one of
implementation, found the problems below in the first draft of this page. This
revision resolves them. The implementation findings marked "verified" were
reproduced against a prototype.

| Finding | Resolution |
| --- | --- |
| A rule exporting a package fact crashed the combined linter, even deselected (verified) | [Separate process](#running-community-rules) |
| "Off unless selected" still ran every declared rule, so a rule could hide or crash core findings (verified) | Separate process; only modules with a selected rule are built |
| The dependency conflict check failed modules that only added dependencies, and kept authors on core versions (verified) | Only the Staticcheck and Go pins are checked; `scripts/test-example-rules` checks exactly that |
| Namespaces with digits joined core categories: `sa1_nopanic` matched `SA1*` (verified) | [Lowercase letters only](#rule-names) |
| The pattern expression rejected `_`, so `acme_nopanic` could not be selected (verified) | [Selection](#selection) updates both copies |
| A consumer's `all` would turn on every community rule | `all` means core rules |
| Analyzer errors were swallowed and passed silently (verified) | Rule failures become [findings](#errors) |
| Result keys cannot include sums resolved inside Dagger | [Keys](#result-keys) use declared entries |
| The lint step shared writable caches and had network access; "no secrets" understated the risk to source | [Security model](#security-model) |
| Import aliases collided with namespaces such as `community` or `type` (verified) | Positional aliases `m0`, `m1`, ... |
| Duplicate names were dropped silently, and `-list-checks` showed blank titles (verified) | Validation at build; `Doc` summary becomes the title |
| Consumer-chosen namespaces made names mean different things in different repositories | Module-owned namespaces; aliases only for collisions |
| Findings did not say where a rule came from or link to its docs | `source` and `url` on every finding |
| No rule settings, advisory mode, upgrade path, rename path, or autofix story | `settings`, `advisory`, [upgrades](#upgrades), [renames](#renames-deprecation-and-graduation), fixes through `singlechecker -fix` |
| Author mistakes surfaced as panics in generated code | [Configuration errors](#errors) naming the module |
| Private modules and native execution were promised but underspecified | Both are explicitly phase 3 |
| No governance, security reporting, or removal policy for the catalog | [Governance](#governance) and `status` |
| "Verified" suggested an audit | "Builds with Levenshtein vX" |
| `levenshtein` as a package name and topic is hard to find | `lvrules` package, `levenshtein-lint-rules` topic |
| The example reported panics in tests and `main_test`, exempted `Mustard`, and missed package-level code (verified) | Fixed, with fixtures that fail when any exemption is removed |
| The example had no license, and its docs URL tracked `main` | `LICENSE` added; the URL points at pkg.go.dev |
