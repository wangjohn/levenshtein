# Community lint rules

This is a proposal, not a description of shipped behavior. It covers how people
outside this repository could publish Go lint rules that Levenshtein consumers
turn on, without those rules entering `runner/lint`, and how a catalog of those
rules could grow over time. The [roadmap](roadmap.md#what-would-justify-expansion)
defers public plugins until they have "actual external consumers"; the
[phasing](#phasing) below keeps to that gate and only commits to the steps that
cost nothing before a consumer asks.

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
| [golangci-lint module plugins](https://golangci-lint.run/docs/plugins/module-plugins/) | `.custom-gcl.yml` lists Go modules; `golangci-lint custom` rebuilds the binary with them compiled in. Plugins call `register.Plugin` and return `[]*analysis.Analyzer` | Module version, verified by `go.sum` and the checksum database | None; popular linters move into core by PR |
| golangci-lint `.so` plugins | Go `plugin` package, loaded at run time | Must match the host's Go version, platform, and every shared dependency exactly | Deprecated in favor of module plugins |
| [TFLint](https://github.com/terraform-linters/tflint/blob/master/docs/user-guide/plugins.md) | Separate binaries speaking gRPC through `go-plugin` | Exact `version` only, no ranges; GitHub artifact attestations or PGP signatures | `tflint-ruleset-*` naming and a template repository |
| [ESLint](https://eslint.org/docs/latest/extend/plugins) | npm packages loaded in process; rules addressed as `namespace/rule` | npm lockfile; `eslint` as a peer dependency | `eslint-plugin-*` naming, npm keywords, awesome-eslint |
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
4. **Namespaced rule IDs**, so two authors cannot both ship a rule with the
   same name: ESLint's `plugin/rule`, RuboCop's `Department/Cop`, Vale's
   `Style.Rule`.
5. **Discovery that grows in steps.** First a naming convention, then a catalog
   curated by PR, and only later a hosted explorer. The catalog is data in
   git, so adding a rule is just a pull request.

## Recommendation

Adopt the golangci-lint module plugin model, adjusted to Levenshtein's pinning
and caching contracts: consumers name community Go modules in `levenshtein.json`,
and the Dagger runner compiles them into a per-consumer `levenshtein-lint`.

### The rule contract

A rule module exposes one package, conventionally `<module>/levenshtein`, with
one function:

```go
package levenshtein

import "golang.org/x/tools/go/analysis"

// Analyzers returns the rules this module contributes.
func Analyzers() []*analysis.Analyzer
```

Each analyzer must set `Name`, `Doc`, and `URL`, because the linter's findings
and `-list-checks` output have nothing else to point readers at. `Name` must be
a Go identifier, which `analysis.Validate` enforces. [Writing a rule module](#writing-a-rule-module)
walks through a complete example. There is no
Levenshtein-specific SDK to import. The same analyzers keep working in
golangci-lint, nogo, and `singlechecker`, so authors are not betting on this
project alone. That also answers the roadmap's concern about a public protocol:
the protocol is `go/analysis`, which Levenshtein does not own or version.

### Configuration

A new optional top-level `rules` object maps a namespace the consumer picks to
an exact module version:

```json
"rules": {
  "acme": {"module": "github.com/acme/levenshtein-rules", "version": "v1.4.0"}
}
```

A `go-lint` check selects the rules with the existing syntax:

```json
"cleanup": {"kind": "go-lint", "target": "app", "environment": "go", "lint": {"checks": ["acme_*"]}}
```

- **Namespacing.** The runner renames each contributed analyzer to
  `<namespace>_<Name>` before registering it. Findings, `//lint:ignore acme_nopanic reason`,
  and selection patterns all use that name, so a community rule can never
  shadow `SA*`, `LV*`, or another module's rules. The consumer picks the
  namespace, as in ESLint's flat config, so two modules that chose the same
  prefix can still be used side by side. The separator is `_`, not ESLint's
  `/`, because analyzer names must be Go identifiers. A namespace is lowercase
  letters and digits with no underscore, so the first `_` always ends it.
- **Namespace globs.** Staticcheck reads a pattern of letters followed by `*`
  as a category, the part of a code before its first digit: `LV*` matches
  `LV1003`. `acme_nopanic` has no digit, so `acme_*` would match nothing. The
  runner already lists the linter's rules to validate patterns, so it expands
  `acme_*` and `-acme_*` into the exact names before calling the linter, and
  `allowed` in `runner/main.go` gets the same rule.
- **Off unless selected.** The shipped selection is `all`, which would include
  community rules. The runner appends `-<rule>` for every declared community
  rule after the shipped selection and before the consumer's patterns. Since
  the last matching pattern wins, a consumer's `acme_*` turns the rules back on.
  Upgrading Levenshtein then never enables a stranger's rule, and declaring a
  module changes no findings until someone selects its rules.
- **Exact versions only.** `version` must be a semantic version tag or a Go
  pseudo-version. Branch names and `latest` are configuration errors, as in
  TFLint and pre-commit. An optional `sum` field (`h1:…`, the `go.sum` hash)
  pins content for private modules that the public checksum database cannot
  verify.
- **Additive to version 1**, like the `lint` object: a file without `rules`
  keeps its meaning and its cached results.

### Build and caching

The `GoLint` Dagger function already builds the linter in a pinned Go image. With
community rules it would:

1. Copy `runner/lint`, `go get` each declared `module@version`, and generate a
   `main.go` that also registers each module's `Analyzers()`. Fetching goes
   through `GOPROXY` with `GOSUMDB` on, so a module whose content differs from
   what the checksum database recorded fails the build.
2. Compare `go list -m all` before and after. If a community module forced a
   different version of any module the core linter requires, such as
   `golang.org/x/tools` or Staticcheck, fail with an error naming the
   conflict. Go's minimum version selection would otherwise silently upgrade
   the dependencies behind the core rules, and a consumer's `SA*` findings
   would change because of an unrelated plugin. golangci-lint documents the
   same requirement for its `.so` plugins but never enforces it.
3. Add the resolved module versions and their `go.sum` hashes to the check's
   result key. Changing `rules` then re-runs the check, and Dagger caches the
   built linter per module set.

The native executor would do the same work in a temporary module. That needs
network access to the proxy on first use, which is a reason to offer this on
Dagger first.

### Trust

A community rule runs arbitrary Go code during `go-lint`, just like every
upstream analyzer Levenshtein already compiles in. The difference is who
reviewed it. The design limits that exposure with mechanisms that already exist:

- The consumer chooses each module and version explicitly. Because the pin is
  exact and checked against the checksum database, an upstream push or a
  replaced tag cannot change what runs.
- The lint step receives the source directory and nothing else: no secrets, no
  host environment. The [roadmap](roadmap.md) warning about sharing writable
  result caches with untrusted PRs applies unchanged.
- The catalog (below) records who maintains each module and which versions its
  CI verified, which gives reviewers something concrete to check.

### Author kit

Publish a `levenshtein-rules-template` repository containing:

- One example analyzer with `analysistest` tests, using `// want` comments for
  findings and clean files for the negatives. The house rules already follow
  the good/bad fixture pattern in `runner/testdata`.
- A CI workflow that builds the module against the latest Levenshtein release
  through the same generated-main path consumers use. Any conflict with the
  core dependency set then surfaces in the author's CI, not in a consumer's.
- The `go list -m all` conflict check as a script authors can run locally.

## Writing a rule module

[`examples/rule-module`](../examples/rule-module) is a complete rule module,
kept outside every Levenshtein module and build. It contributes one rule,
`nopanic`, which reports the builtin `panic` in library code. Its layout is what
the template repository would ship:

```text
go.mod                          module github.com/wangjohn/levenshtein/examples/rule-module
levenshtein/levenshtein.go      Analyzers(), the only package Levenshtein imports
nopanic/nopanic.go              the rule, an ordinary analysis.Analyzer
nopanic/nopanic_test.go         analysistest over the fixtures below
nopanic/testdata/src/...        library (findings), app and shadowed (no findings)
cmd/nopanic/main.go             singlechecker, for running the rule today
```

### The rule

A rule is an `analysis.Analyzer` like every upstream analyzer in
`runner/lint`. `Name` is local to the module; the consumer's namespace is added
later. `URL` is where findings send readers.

```go
var Analyzer = &analysis.Analyzer{
	Name:     "nopanic",
	Doc:      "return an error from library code instead of calling panic",
	URL:      "https://github.com/wangjohn/levenshtein/tree/main/examples/rule-module#nopanic",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}
```

`run` skips package `main`, `init`, and `Must*` functions, and reports every
other call whose callee type-checks as the builtin `panic`. Because it uses
type information, a local function named `panic` is not reported, which a
text-matching rule would get wrong:

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

`levenshtein/levenshtein.go` is the only package Levenshtein imports. It is the
equivalent of golangci-lint's `register.Plugin` call, with no import of
Levenshtein:

```go
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		nopanic.Analyzer,
	}
}
```

### Tests

Fixtures under `testdata/src` mark each expected finding with a `// want`
comment, and `analysistest` fails on any finding without one:

```go
func Parse(input string) int {
	if input == "" {
		panic("empty input") // want `Parse panics; return an error so callers can handle the failure`
	}
	return len(input)
}

func MustParse(input string) int {
	count, err := ParseChecked(input)
	if err != nil {
		panic(err)
	}
	return count
}
```

```go
func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), nopanic.Analyzer, "library", "app", "shadowed")
}
```

Removing the `Must*` and `init` exemption makes the test fail with two
unexpected diagnostics, which is how the fixtures were checked to have teeth.

### Running it before Levenshtein supports modules

`cmd/nopanic` wraps the rule in `singlechecker`, so a consumer can run it now
(phase 0) as a native `command` check. `v0.1.0` stands for a published tag; this
example is not tagged, so it is a shape to copy rather than a working pin:

```json
"nopanic": {"kind": "command", "target": "app", "environment": "host", "command": {"args": ["go", "run", "github.com/wangjohn/levenshtein/examples/rule-module/cmd/nopanic@v0.1.0", "./..."], "rerun_args": ["go", "run", "github.com/wangjohn/levenshtein/examples/rule-module/cmd/nopanic@v0.1.0", "./..."]}}
```

That gives up the JSON findings, the shared selection syntax, and
`//lint:ignore`, which is what phase 1 adds.

## What the runner adds

These pieces were prototyped against a scratch copy of `runner/lint` and are
not in this repository. The copy built with `examples/rule-module` compiled in,
`runner/lint/go.mod`'s requirements did not change, and on a sample package:

- `-list-checks` listed `acme_nopanic`.
- With the shipped selection and `-acme_nopanic` appended, it reported nothing.
- Adding `acme_nopanic` reported `store.go:7:3: Load panics; ... (acme_nopanic)`,
  with `"code":"acme_nopanic"` in `-f=json` output.
- A `//lint:ignore acme_nopanic <reason>` line silenced the site below it.

A new package, `runner/lint/community`, holds the registry. It is the only core
code that knows community rules exist:

```go
// Register adds a module's analyzers under namespace, renamed to
// <namespace>_<name>. Analyzer names must be Go identifiers, which rules out
// a slash; the prefix keeps them apart from SA*, LV*, and other modules.
func Register(namespace string, analyzers []*analysis.Analyzer) {
	if !namespaces.MatchString(namespace) {
		panic(fmt.Sprintf("community rule namespace %q must be lowercase letters and digits", namespace))
	}

	for _, analyzer := range analyzers {
		if analyzer.Doc == "" || analyzer.URL == "" {
			panic(fmt.Sprintf("community rule %s_%s must set Doc and URL", namespace, analyzer.Name))
		}

		renamed := *analyzer
		renamed.Name = namespace + "_" + analyzer.Name
		registered = append(registered, &renamed)
	}
}

// Analyzers returns every registered community rule, adapted like the
// upstream analyzers so generated files stay silent.
func Analyzers() []*analysis.Analyzer {
	return policy.Adapt(registered...)
}
```

`main.go` gains one line after the house rules:

```go
command.AddBareAnalyzers(community.Analyzers()...)
```

For each consumer, the `GoLint` Dagger function writes one file from the
`rules` object, runs `go get` for each module, checks `go list -m all` against
the core set, and builds:

```go
// Code generated by levenshtein from levenshtein.json; DO NOT EDIT.

package main

import (
	acme "github.com/acme/levenshtein-rules/levenshtein"
	"github.com/wangjohn/levenshtein/runner/lint/community"
)

func init() {
	community.Register("acme", acme.Analyzers())
}
```

Without a `rules` object, no file is generated and the linter is built exactly
as it is today.

## The catalog

Keep the catalog in a separate repository, such as `levenshtein-rules-catalog`.
Community rules then never pass through this repository's review queue or
release cadence.

- **Data.** One `catalog.json` whose entries record a suggested namespace, the
  module path, a description, the license, maintainers, each rule's name,
  `Doc`, and `URL`, and the most recent version that passed verification. A
  suggested namespace is reserved first-come, like an npm scope, so the
  catalog's `acme_...` names mean the same thing across repositories.
- **Admission by PR.** Catalog CI resolves the module, runs its tests, builds
  it against the current Levenshtein release, runs the dependency conflict
  check, and requires `Name`, `Doc`, `URL`, a test with at least one finding
  and one clean case, and an OSI license. Semgrep's registry shows why
  that last point matters: its rules license stops people reusing registry
  rules.
- **Evidence, not only presence.** CI also runs each rule over a fixed corpus,
  starting with this repository and `runner/testdata/good`, and records the
  number of findings. `docs/checks.md` already measures every upstream rule
  this way before turning it on, and catalog readers deserve the same numbers.
- **Staying current.** A scheduled job re-verifies every entry against each
  new Levenshtein release and marks the entries that stop building. Consumers
  can then tell a dormant module from a broken one.
- **Discovery.** A static page generated from `catalog.json`, the GitHub topic
  `levenshtein-rules`, and the `levenshtein-rules-*` repository naming
  convention. Adoption can be counted from public `levenshtein.json` files
  that reference a module, through code search or GitHub's dependency graph;
  Levenshtein itself collects no telemetry.

### Graduation into core

The catalog also feeds the core selection. When a community rule has users in
several unrelated repositories, clear fixtures, and no overlap with a shipped
rule, propose it for `runner/lint` using the existing standard: a recurring
defect, known-bad and legitimate fixtures, repair guidance, and measured
cost. ESLint core rules and golangci-lint's default linter list grew the same
way. The catalog is where rules prove themselves before the main build pays to
maintain them.

## Alternatives considered

| Option | Why it is not the recommendation |
| --- | --- |
| Go `.so` plugins | The version skew that led golangci-lint to deprecate them; no Windows support |
| Separate vet tools (`go vet -vettool`, one `unitchecker` binary per module) | Works with no core changes and isolates crashes. But every module type-checks the program again, findings leave Levenshtein's JSON report and `-checks` selection, and `//lint:ignore` stops applying. A good fallback if the dependency conflict check proves too strict in practice |
| RPC plugins, as in TFLint | Isolates processes, but means designing and versioning a protocol for something `go/analysis` already defines |
| Declarative rules only (Semgrep, ruleguard) | Lower risk and easy to review, but cannot express type-aware rules like `LV1001` or `LV1006`. It could be a second tier later: `go-ruleguard` bundles are also Go modules, so the same `rules` object could carry them |
| A catalog in this repository | Puts every community rule in the main repository's review queue, which is what this proposal exists to avoid |

## Phasing

| Phase | Work | Gate |
| --- | --- | --- |
| 0 | Document the `analysis.Analyzer` contract, publish the template repository (starting from `examples/rule-module`), and document the `command` check as the interim way to run it | None; costs no core code |
| 1 | `rules` config, generated-main build, namespacing, dependency conflict check, and result keys, on the Dagger executor | A pilot consumer asks to run a rule Levenshtein will not ship |
| 2 | Catalog repository, admission CI, scheduled re-verification | A second, unrelated rule module exists |
| 3 | Generated catalog page, corpus findings counts, native executor support | The catalog lists enough entries that browsing `catalog.json` is inconvenient |
