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
  `Namespace` and returns `[]*analysis.Analyzer`. It does not import Levenshtein,
  so the same rules also work in golangci-lint, nogo, and `singlechecker`.
- **Consumers** pin the module by exact version in `levenshtein.json` and say
  which of its rules to run:

  ```json
  "rule_modules": {
    "github.com/acme/lvrules-errors": {"version": "v1.4.0", "namespace": "errs", "select": ["errs_*"]}
  }
  ```

- **Levenshtein** builds the selected rules into a second linter binary on the
  same pinned Staticcheck, runs it next to the core linter, and merges both into
  one report. Each community finding names its module and links to its docs.
  Rules can be advisory, which means they report without failing the check.
- **The catalog**, in its own repository, lists modules that build with the
  current release, pass their tests, and have an owner. Rules that prove
  themselves there can graduate into the core selection.

## Terms

| Term | Meaning |
| --- | --- |
| Check | A Levenshtein check in `levenshtein.json`, such as a `go-lint` check. It passes, fails, or errors as a whole |
| Rule | One `analysis.Analyzer`, reported under one code |
| Code | The name a finding reports and `//lint:ignore` uses: `SA4006`, `LV1003`, or `errs_nopanic` |
| Finding | One diagnostic a rule reports at one location |
| Rule module | A Go module with an `lvrules` package |
| Namespace | The prefix of every code a rule module contributes |
| Core linter | `levenshtein-lint`, built from `runner/lint` |
| Community linter | `levenshtein-community-lint`, built per configuration from the selected rule modules |

## Where things stand

Every rule a `go-lint` check can report is compiled into the core linter, which
`runner/lint/cmd/levenshtein-lint/main.go` assembles from Staticcheck, upstream
`go/analysis` analyzers, and the house `LV*` rules in `runner/lint/policy`. The
runner builds it inside Dagger from the pinned shared checkout. A consumer's
`lint.checks` can only switch registered rules on or off. Today the only way to
run a rule Levenshtein does not ship is a native `command` check, which gives up
the shared selection syntax, `//lint:ignore`, JSON findings, and Dagger result
caching.

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
   caches by `rev` on the assumption that it never moves. Go retractions and
   crates.io yanks warn, and never break a build that already pins the version.
4. **Rule IDs owned by the plugin.** ESLint's `plugin/rule`, RuboCop's
   `Department/Cop`, and Vale's `Style.Rule` mean the same thing in every
   repository, so documentation, ignore comments, and answers online all carry
   over.
5. **Discovery that grows in steps.** First a naming convention, then a catalog
   curated by PR, and only later a hosted explorer. The catalog is data in
   git, so adding a rule is just a pull request.

## Rule modules

A rule module is any Go module with an `lvrules` package that exports:

```go
package lvrules

import "golang.org/x/tools/go/analysis"

// Namespace prefixes every rule this module contributes. Required.
const Namespace = "errs"

// Analyzers returns the rules this module contributes. Required.
func Analyzers() []*analysis.Analyzer

// Renamed maps a rule's old name to its current one. Optional; see
// "Renames, deprecation, and graduation".
var Renamed = map[string]string{"panics": "nopanic"}
```

The module does not import Levenshtein. The protocol is `go/analysis`, which
Levenshtein does not own or version, so there is no Levenshtein SDK to keep
compatible.

**Contract stability.** The contract grows only by optional exports that a
module can leave out. If it ever needs a breaking change, that change gets a
new package name, and the runner supports both names for at least a year. In
phase 0 the contract is marked unstable, and it becomes stable when phase 1
ships.

Each analyzer must meet these requirements. The community linter build rejects
a module that breaks one of the first three (see [errors](#errors)); catalog
admission checks the fourth:

- **`Name`** is a Go identifier that `analysis.Validate` accepts and is unique
  within the module, ignoring case.
- **`Doc`** starts with a one-line summary, followed by a blank line if there
  is more. `-list-checks` and the catalog show that line.
- **`URL`** links to the rule's documentation: what it reports, why, how to fix
  it, and how to suppress it. Link to a stable location, such as pkg.go.dev or
  a docs page for each release.
- **Not a copy of a core rule.** A module that returns `nilness.Analyzer`, for
  example, reports every finding twice (see [admission](#admission)).

Rule metadata stays in the `go/analysis` fields rather than a Levenshtein
metadata type, which would force modules to import Levenshtein. The catalog
generates its list of rules from the built module, so it cannot drift from the
code.

The module must also build with the Go version in `.go-version` and the
Staticcheck version in `runner/toolchain.json`. It may require newer versions
of other shared dependencies, such as `golang.org/x/tools`, as long as
Staticcheck still builds with them. Community rules run in their own process,
so a newer `x/tools` never reaches the core rules. Authors should still require
the lowest versions their rules need, which keeps the module usable with older
Levenshtein releases.

Rules may use analysis facts and suggested fixes. Levenshtein reports findings
and does not apply fixes; a module's own `singlechecker` wrapper applies them
with `-fix`.

## Rule names

A rule reports as `<namespace>_<name>`, such as `errs_nopanic`. That one code
appears in findings, in `select` and `lint.checks` patterns, and in
`//lint:ignore errs_nopanic <reason>`.

- **The publisher owns the namespace.** It is part of the published interface,
  like an ESLint plugin name, so `errs_nopanic` means the same rule in every
  repository. The catalog records each namespace against one publisher entry,
  which can list several module paths, including a module's `/v2` and later
  major versions.
- **Name it after the topic, not the organization.** `errs` or `sqlsafe`, not
  `acme`, so one organization can publish several modules.
- **Lowercase letters only.** Staticcheck reads a pattern of letters followed
  by `*` as a category: the part of a code before its first digit, compared
  ignoring case. A namespace with a digit would join a core category: namespace
  `sa1` would make `SA1*` select `sa1_nopanic`. With letters only, and a `_`
  that no core rule uses, a community code can never match a core pattern.
- **`_`, not `/`.** Analyzer names must be Go identifiers.
- **Reserved:** `lvrules` (Levenshtein's own diagnostics about community rules,
  below), `example` (the example module), `levenshtein`, and `lv`.
- **Aliases resolve collisions only.** Two uncatalogued modules can claim the
  same namespace. A consumer then sets `alias` on one of them. Patterns and
  `//lint:ignore` directives use the alias, and every finding from that module
  still names its module in `source`.

## Consumer configuration

A new optional top-level `rule_modules` object maps a module path to how the
repository uses it:

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

| Field | Meaning |
| --- | --- |
| `version` | Required. A semantic version tag or Go pseudo-version. Branch names and `latest` are configuration errors |
| `namespace` | Required. The module's `Namespace`, repeated here so the CLI can route and validate patterns before anything is built. The build fails if it differs from the module |
| `select` | Required, nonempty. Patterns in the `lint.checks` syntax for the rules every `go-lint` check runs from this module. They may only name this module's namespace. To stop using a module, remove its entry |
| `advisory` | Patterns for selected rules whose findings are reported without failing the check. Naming a rule no check selects is an error |
| `settings` | Flag values for each rule, set through the analyzer's `Flags`, the way `runner/lint` configures `gocognit` and `usetesting`. Values are strings, as on a command line |
| `alias` | Replaces `namespace` in this repository's patterns and directives when two modules collide |

`advisory` and `settings` apply to the module wherever its rules run. A check
changes which rules run through `lint.checks`, and opts out of community rules
entirely with `"lint": {"rule_modules": false}`. `rule_modules` is an additive,
optional field of version 1, like `lint`, so a file without it keeps its meaning
and its cached results.

### Selection

For each `go-lint` check, the runner builds one pattern list in this order, and
the last pattern that matches a rule wins, as it does today:

1. the shipped core selection from `runner/toolchain.json`;
2. `-` for every community rule, so nothing is on by default;
3. each module's `select`;
4. the check's own `lint.checks`.

Patterns then split by owner. Patterns containing `_` go to the community
linter and everything else to the core linter. Patterns are compared ignoring
case, as Staticcheck does.

| Pattern in `lint.checks` | Effect |
| --- | --- |
| `all`, `*` | Every core rule. No community rule, so adding a module never changes what `all` means |
| `-all`, `-*` | Turns every core rule off. Community rules are unchanged |
| `errs_*` | Every rule in the `errs` namespace |
| `errs_no*` | Every `errs` rule starting with `no`. A pattern containing `_` and ending in `*` is a literal prefix |
| `-errs_nopanic` | Turns that rule off for this check only |
| `other_*` | Configuration error: no declared module has the namespace `other` |

The runner expands community globs into exact codes, because Staticcheck's
category rule would otherwise match nothing. The pattern expression in
`runner/main.go` and `internal/verify/validation.go` (today
`^-?(\*|[A-Za-z][A-Za-z0-9]*\*?)$`) must accept `_` in both copies.

### Advisory findings

An advisory rule reports like any other rule, but its findings do not fail the
check. The community linter runs with Staticcheck's `-fail` flag listing only
the failing rules. Advisory findings then come back with severity `warning` and
exit code 0, which a prototype confirmed. A check whose only findings are
advisory passes, and its result still carries those findings. That needs a
change: today a passing Dagger check returns no details, and `parseFindings`
rejects findings with exit code 0. `GoLint` would return a JSON summary for
passing results too, as `go-mutation` does. Cached passes replay their findings
from the stored details.

A stale `//lint:ignore` directive is advisory when every code it names is
advisory, and fails the check otherwise.

The job summary in `action.yml` prints advisory findings under each check:

```text
| `go-lint/app` | passed | 2 advisory |
  [advisory] store/store.go:7:3 errs_sentinel: compare errors with errors.Is (https://pkg.go.dev/github.com/acme/lvrules-errors/sentinel)
```

and the documented `jq` recipe for reading reports gains a line:

```sh
jq -r '.results[] | .findings[]? | "\(if .advisory then "[advisory] " else "" end)\(.location.file):\(.location.line) \(.code): \(.message) \(.url // "")"' report.json
```

### Executors

In phases 1 and 2, community rules run only on the Dagger executor. A native
`go-lint` check runs its core rules as it does today and skips community rules
with a `rule-modules-skipped` [warning](#warnings) naming the modules it skipped.
It does not fail, so a configuration with both executors, like this
repository's `native-go-lint` and `go-lint`, can adopt community rules. Native
support is planned for phase 3, under the same trust rules as native `command`
checks (see [security model](#security-model)). `go-http` and `go-sql` keep
their single rules and never run community rules.

## Running community rules

Community rules run in the community linter, never inside the core linter. A
prototype confirmed these reasons:

| In one binary | In a separate binary |
| --- | --- |
| A community analyzer that exports a package fact crashed the whole linter with `interface conversion: analysis.Fact is *levenshtein.pkgFact, not *deprecated.IsDeprecated`, because Staticcheck's runner hands every package fact to its own `deprecated` analyzer. This happened even with the rule deselected | The same analyzer ran cleanly |
| A module could raise `golang.org/x/tools` under the core rules through minimum version selection and silently change core findings. Preventing that meant authors could never require a newer dependency than the pinned linter | Only Staticcheck and Go must stay at their pins. Other shared dependencies may move, and they only affect community rules |
| A community rule that crashes, writes to stdout, or calls `os.Exit(0)` takes core findings with it; `os.Exit(0)` makes the check pass | Core findings are unaffected. The community half fails with an error naming its modules, and a completion marker catches an early `os.Exit(0)` (see [errors](#errors)) |
| One package load | A second package load. On this repository's root module (4 CPUs, cold Staticcheck cache), the core linter took about 6.5 s and a community linter with two rules about 2.5 s, almost all of it loading. Checks without community rules pay nothing |

### Ignore directives

A `//lint:ignore` or `//lint:file-ignore` directive that names only core codes,
or only community codes, works as it does today. Staticcheck reports an unused
directive only when it names a selected rule. So the core linter never flags
`//lint:ignore errs_nopanic`, the community linter never flags
`//lint:ignore SA4006`, and each still flags stale directives for its own rules.

A directive that mixes the two, such as `//lint:ignore SA4006,errs_nopanic reason`,
cannot work. Each linter judges only its own codes, so whichever half matched
nothing is reported as unused, even while the other half is still needed. A
prototype showed both directions. The community linter therefore reports a
mixed directive as `lvrules_mixed`:

```text
store.go:14:2: split this directive: core and community rules run in separate linters; write one //lint:ignore for SA4006 and another for errs_nopanic on consecutive lines (lvrules_mixed)
```

The runner drops both linters' unused-directive reports at that position.
Consecutive single-owner directives work, which a prototype confirmed.

### Findings

The runner merges both linters' JSON into the check's findings and adds three
fields to every community finding:

```json
{
  "code": "errs_nopanic",
  "message": "Load panics; return an error so callers can handle the failure",
  "location": {"file": "store/store.go", "line": 7, "column": 3},
  "source": "github.com/acme/lvrules-errors@v1.4.0",
  "url": "https://pkg.go.dev/github.com/acme/lvrules-errors/nopanic",
  "advisory": false
}
```

Core findings gain `url` and `advisory` as well: Staticcheck's own rules have
pages at `https://staticcheck.dev/docs/checks#<code>`, and the rest can link to
their row in [shared checks](checks.md#go-lint-rules). `-explain` cannot help,
because `lintcmd` prints a staticcheck.dev link for every rule, community rules
included. The `url` field is where the documentation link lives.

Both linters report compile errors in the consumer's code under the `compile`
code, and directive problems under `staticcheck`. The runner reports each
once, and accepts both codes from the community linter as well as the core one.

### Warnings

Several conditions need to reach a person without failing anything, and the
report has nowhere to put them today. Each result gains a versioned `warnings`
list of `{code, message}` objects. It is stored with cached results and replayed
from them, and the job summary prints it.

| Code | When |
| --- | --- |
| `rule-modules-skipped` | A native `go-lint` check skipped community rules |
| `rule-module-deprecated` | The pinned module version is deprecated in this Levenshtein release's [module list](#withdrawn-and-deprecated-modules) |
| `rule-module-retracted` | The module's author retracted the pinned version (`go list -m -retracted`, checked during the build) |
| `rule-renamed` | A pattern names a rule by an old name that the module's `Renamed` maps |
| `rule-deprecated` | A selected rule's `Doc` has a `Deprecated:` paragraph |

Retraction is checked when the community linter is built, so a cached result
does not notice a later retraction. `./verify rules` (see [upgrades](#upgrades))
checks live.

### Errors

Problems are reported where they can be fixed, never as a Go stack trace from
generated code. What can be checked depends on when the check runs:

- **When configuration is loaded (exit 2).** The CLI knows only
  `levenshtein.json`, so it checks:
  - version syntax;
  - namespace and alias syntax;
  - namespaces declared twice;
  - patterns in `select`, `advisory`, `settings`, or `lint.checks` that name
    an undeclared namespace;
  - a module version that this Levenshtein release lists as
    [withdrawn](#withdrawn-and-deprecated-modules).
- **When the community linter is built (check `error`, exit 1).** These need
  the module's source, which only exists inside Dagger. The check's error
  names the module:
  - `namespace` that differs from the module's `Namespace`;
  - a selected, advisory, or configured rule that does not exist;
  - an unknown setting;
  - a missing `URL`;
  - duplicate names;
  - an analyzer another module also returns;
  - an incomplete `go.mod`;
  - a Go or Staticcheck version above the pins;
  - a module that does not compile with the pinned Staticcheck's
    dependencies.

  Messages state what is known and never guess a fix:

  ```text
  github.com/acme/lvrules-errors@v1.5.0 requires honnef.co/go/tools v0.9.0 (through github.com/acme/lvrules-errors -> example.com/helper@v0.3.0), but this Levenshtein release pins v0.8.1. Use a version of the module that supports v0.8.1, or a Levenshtein release that pins v0.9.0 or later.
  ```

- **When a rule fails (check `error`, exit 1).** Staticcheck's runner records
  an analyzer's returned error and exits 0. A prototype analyzer that returned
  an error produced no output at all, which is a silent pass. The community
  linter wraps every analyzer, including those reached only through `Requires`.
  A returned error or a panic is written to a failure file, not reported as a
  finding, and the analyzer returns the error so its dependents never see a nil
  result. The runner reports each failure once: "errs_nopanic
  (github.com/acme/lvrules-errors@v1.4.0) failed on 3 packages: ...". The core
  linter needs the same fix.
- **When the community linter exits early.** It calls `lintcmd`'s `Execute`
  rather than `Run` and writes a completion marker after it returns. A missing
  marker means a rule exited the process, whatever the exit code. Core findings
  are still reported in every case.

## Build

For a `go-lint` check that runs community rules, the `GoLint` Dagger function
gains a `rule_modules` argument and works in three containers:

1. **Build (network).** Generates a module that requires the pinned
   Staticcheck and each `module@version`, runs `go mod tidy` with `GOPROXY` and
   `GOSUMDB` on, and builds the community linter with `GOTOOLCHAIN=local`.
   Content that differs from what the checksum database recorded fails the
   build. So does a `go mod tidy` that reports "finding module for package":
   that means a module imports a package no `go.mod` requires, and the result
   would depend on what is newest on the proxy that day. The build also fails
   if resolution moved Staticcheck off its pin or a module needs a newer Go.
2. **Download (network).** Runs `go mod download` for the consumer's module,
   verified against its `go.sum`, and exports the module sources as a directory.
3. **Lint (no shared state).** Starts from the pinned Go image, because
   Staticcheck runs `go list`. It adds the community linter binary and the
   downloaded sources as plain directories, sets `GOPROXY=off`,
   `GOFLAGS=-mod=readonly`, and a fresh `GOCACHE`, and uses a Staticcheck cache
   volume of its own. See [security model](#security-model).

The generated `main` registers only rules that some check selects, because
Staticcheck runs every registered analyzer and filters findings afterwards. It
imports modules under positional aliases, so a namespace can never collide with
an identifier in the generated file:

```go
// Code generated by levenshtein from levenshtein.json; DO NOT EDIT.

package main

import (
	m0 "github.com/acme/lvrules-errors/lvrules"
	"github.com/wangjohn/levenshtein/runner/community"
)

var modules = []community.Module{{
	Path:      "github.com/acme/lvrules-errors",
	Version:   "v1.4.0",
	Namespace: m0.Namespace,
	Renamed:   m0.Renamed,
	Analyzers: m0.Analyzers(),
	Selected:  []string{"nopanic", "sentinel"},
	Settings:  map[string]map[string]string{"nopanic": {"allow": "Must,Should"}},
}}
```

A module without a `Renamed` export gets `nil` in its place. `runner/community`
is a new module with the same Staticcheck pin as `runner/lint` and no other
connection to it. It:

- validates each module;
- clones every analyzer and the whole `Requires` graph beneath it, once per
  pointer, so that renaming and the failure wrapper reach dependencies too;
- applies settings through each clone's `Flags`;
- gives `Doc` a title line only when it has none, since `lintcmd` splits the
  title at the first blank line;
- registers the lint-only analyzers `lvrules_mixed` and `lvrules_renamed`;
- passes the result to `lintcmd`.

Settings live on each analyzer's `FlagSet`, which a copied analyzer still
shares. So two modules that return the same analyzer pointer would share its
settings, and the build rejects that case.

### Result keys

The CLI computes result keys before any executor runs, so they cannot include
what `go mod tidy` resolves inside Dagger. The key includes the declared
`rule_modules` entries (module, version, namespace, `select`, `advisory`,
`settings`, `alias`) and the shared implementation, as it already does for
`lint.checks`. Since the build refuses any resolution that depends on the
proxy's current state, those inputs determine the resolved graph. Changing any
of them re-runs the check, and Dagger caches the community linter build under
the same inputs.

## Security model

A community rule is code execution with read access to your source. The design
limits what it can reach, but it cannot remove that risk by review or by
pinning:

- **Nothing shared is writable.** The lint step does not mount the Go module
  cache, the build cache, or the core linter's Staticcheck cache volume. A rule
  cannot poison results for other checks, or for other consumers on the same
  engine.
- **No credentials in the lint step.** Anything the build or download needs,
  now or later, stays in those containers. Only the binary and the module
  sources are copied out.
- **Pins that cannot move.** An exact version checked against the checksum
  database means an upstream push or a replaced tag cannot change what runs.
- **Core findings stay honest.** A misbehaving rule cannot hide or rewrite core
  findings, which come from another process.
- **The network is still reachable, for now.** The pinned Dagger (v0.21.9) has
  no option to disable networking for one command; `ContainerWithExecOpts` has
  none, and `sdk/patched-go` adds none. Until Dagger offers one, a community
  rule can send the source it reads elsewhere. Running `unshare --net` inside
  the container would need `InsecureRootCapabilities`, which grants more than
  it takes away, and is untested here. So until the lint step has no network,
  the documentation must say plainly: **enable a community rule on a private
  repository only if you would run its author's code against that repository
  yourself.**
- **Native execution is trusted-only.** When phase 3 adds native support,
  community rules run on the host with the user's access. `SECURITY.md` gets a
  section alongside "Not a sandbox" for `command` checks.

`SECURITY.md` should also say that vulnerabilities in a community rule go to
that module's maintainers, and a malicious module to the catalog maintainers
(see [governance](#governance)). Until then, Levenshtein's maintainers will
receive those reports first.

### Private modules

Private rule modules are out of scope until phase 3: the build has no way to
receive credentials, and the checksum database cannot verify a private module.
A later design would pass a token as a Dagger secret to the build container only,
set `GOPRIVATE` for the module's path, and require a `sum` field (the `h1:`
hash) in the entry, so the pin still verifies content.

## Upgrades

An exact pin in `levenshtein.json` is invisible to Dependabot and Renovate
unless they are told where to look. Ship a Renovate preset built on its
[JSONata custom manager](https://docs.renovatebot.com/modules/manager/jsonata/),
which reads each `rule_modules` entry whatever order its keys are in:

```json
{
  "customManagers": [{
    "customType": "jsonata",
    "fileFormat": "json",
    "managerFilePatterns": ["/(^|/)levenshtein\\.json$/"],
    "matchStrings": ["$each(rule_modules, function($entry, $module) { {\"depName\": $module, \"currentValue\": $entry.version} })"],
    "datasourceTemplate": "go"
  }]
}
```

Test the preset against a real repository before phase 2 ships it. Rule
authors publish GitHub releases with notes, which Renovate links in its pull
requests. A `./verify rules` command lists each declared module with its pin,
newer versions, retractions, and catalog status. It fetches that information
live, and never as part of a check.

## Renames, deprecation, and graduation

Rule names end up in ignore directives across many repositories, so a rename
must produce one clear message, not a wave of "this linter directive didn't
match anything":

- **Within a module.** A module that renames a rule lists the old name in
  `Renamed`, which makes the rename a minor version. Removing a rule, or
  renaming one without `Renamed`, needs a new major version. The runner maps
  old names in patterns with a `rule-renamed` warning. Staticcheck's directive
  matching cannot be extended, so an old-name directive no longer suppresses
  anything. Instead, the lint-only rule `lvrules_renamed` reports it: "`errs_panics`
  is now `errs_nopanic`; update this directive". The fix is one edit on the
  line the message names.
- **Into core.** When a community rule graduates, the core linter gets the same
  kind of lint-only rule for graduated codes. It keeps reporting old-code
  directives, with the new `LV` code, for at least six months and two releases.
- **Deprecation.** A rule whose `Doc` has a `Deprecated:` paragraph, the Go
  convention for deprecation notices, gets a `rule-deprecated` warning on every
  check that selects it.

## Withdrawn and deprecated modules

The runner never fetches the catalog while running a check. Checks stay
reproducible: a run on the same commit with the same pins gives the same answer
as before. Each Levenshtein release instead carries
`runner/rule-modules.json`, a list of module versions the catalog has marked
`withdrawn` (malicious or dangerous) or `deprecated`, taken from the catalog at
release time:

- A **withdrawn** version is a configuration error, but only once the consumer
  updates their Levenshtein pin. Until then, the catalog's advisory, the
  release notes, and `./verify rules` are how they hear about it.
- A **deprecated** version adds a `rule-module-deprecated` warning.

The list is part of the shared implementation, so it is already in every
result key.

## Writing a rule module

[`examples/rule-module`](../examples/rule-module) is a complete rule module,
kept outside every Levenshtein module and build. Its namespace is `example`,
reserved for it, and it contributes one rule, `example_nopanic`, which reports
the builtin `panic` in library code.

### Your first rule

1. **Scaffold.** `gonew github.com/wangjohn/lvrules-template github.com/you/lvrules-errors`
   copies the template (published in phase 0 from `examples/rule-module`) and
   rewrites its module path. Choose a namespace named after the topic. From
   phase 2, check the catalog to see whether someone has claimed it. The
   template's CI fails while the namespace is still `example`.
2. **Write the rule** as an `analysis.Analyzer` with `Name`, `Doc`, and `URL`,
   and add it to `Analyzers()`.
3. **Write fixtures** under `testdata/src` with a `// want` comment on every
   expected finding, and clean files for the cases the rule must not report.
   Check that the test fails when you break the rule.
4. **Build it against Levenshtein.** The template's CI runs the reusable
   `lvrules-check` action. It builds the module into a community linter for each
   Levenshtein release listed in its `levenshtein` input, checking the Go and
   Staticcheck pins, and runs it on the fixtures. This is the release-agnostic
   form of `scripts/test-example-rules`.
5. **Release.** Tag `v0.1.0`, write release notes, add the
   `levenshtein-lint-rules` topic to the repository, and name it
   `lvrules-<topic>`.
6. **List it** (phase 2). Open a pull request to the catalog with the
   publisher entry.

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

`go.mod` requires Go 1.24 and `golang.org/x/tools` v0.40.0, the lowest versions
the rule needs, not the versions Levenshtein pins.

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
`panic`, so a local function named `panic` is not reported. It skips package
`main`, `_test.go` files, generated files, `init` functions, and `Must`
functions. A panic inside a function literal is attributed to the declared
function around it, and a panic in package-level code is reported as such:

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

It still reports `panic("unreachable")` for impossible states. A consumer that
uses that idiom suppresses it with `//lint:ignore` and a reason, or makes the
rule advisory.

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

The fixtures cover:

- `Must` and `MustParse` versus `Mustard`;
- `init` versus a method named `init`;
- closures and package-level function literals;
- `_test.go` files, in a library package and in a `main_test` package;
- generated files;
- a shadowed `panic`.

Each exemption was checked by removing it and watching the test fail.

### Running it today

Before phase 1, a consumer can run a rule as a native `command` check through
its `singlechecker` wrapper, pinned to an exact version. That gives up merged
findings, selection, and `//lint:ignore`:

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
  - fails if the module's `go` directive is newer than `.go-version`;
  - runs the module's tests with `-race`;
  - builds a community linter from it on the Staticcheck version
    `runner/toolchain.json` pins, failing if the module would move that pin or
    if resolution needs a module no `go.mod` requires;
  - runs the linter on a sample package, expecting exactly one
    `example_nopanic` finding and a working `//lint:ignore example_nopanic`.

The script builds its linter with a plain rename, not `runner/community`, which
does not exist yet. Phase 1 switches it to the real registration so CI covers
the validation and failure wrapper too. Dependabot watches `runner/lint` but
not the example, which should keep requiring the lowest versions it needs.

## The catalog

The catalog lives in its own repository, `levenshtein-lint-rules`, so community
rules never pass through this repository's review queue or release cadence.
It is run like any other open source project, with its own `README`,
`CONTRIBUTING`, `GOVERNANCE`, `SECURITY.md`, `CODE_OF_CONDUCT`, and
`CODEOWNERS`.

### Entries

One `catalog.json` lists every publisher:

```json
{
  "namespace": "errs",
  "modules": ["github.com/acme/lvrules-errors", "github.com/acme/lvrules-errors/v2"],
  "description": "Error handling rules for library packages",
  "license": "MIT",
  "maintainers": ["@acme-dev", "@acme-ops"],
  "builds_with": {"module": "github.com/acme/lvrules-errors/v2", "version": "v2.0.1", "levenshtein": "v0.3.0", "checked": "2026-10-01"},
  "status": "active",
  "replacement": null
}
```

Rule names, summaries, and URLs are not typed into the entry. Admission CI
generates them from the built module into a separate `rules.json`.

| `status` | Meaning | Runner behavior |
| --- | --- | --- |
| `active` | Builds with the current release and has an owner | None |
| `broken` | Has not built with any Levenshtein release for 90 days. Set by the scheduled job, and cleared when it builds again | None; the catalog page shows it |
| `deprecated` | The maintainers point users elsewhere, named in `replacement` | `rule-module-deprecated` warning, after the consumer's next Levenshtein update |
| `withdrawn` | Malicious or dangerous | Configuration error, after the consumer's next Levenshtein update |

### Admission

Catalog CI runs on `pull_request` with a read-only token and no secrets, on
GitHub-hosted runners, because it builds and runs code from strangers. It:

- resolves the module and runs its tests;
- builds it into a community linter against the current Levenshtein release,
  checking the Go and Staticcheck pins;
- checks that the module's `Namespace` matches the entry and that no other
  entry holds that namespace;
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

A catalog maintainer approves every new namespace, and every version bump
that changes the module's `go.mod`. Other updates by an entry's listed
maintainers merge once CI passes. That means a compromised author account can
change rule code without human review, and the catalog page says so.

### What readers see

A static page generated from `catalog.json` and `rules.json` shows for each
publisher:

- its rules, with summaries and links;
- the latest version that built, labeled "builds with Levenshtein v0.3.0".
  The label is deliberately not "verified": the catalog checks that a module
  builds and passes its tests, not that its code has been audited;
- the date of its last release and last successful check;
- its corpus finding counts;
- how many public repositories reference it, counted through code search.
  Levenshtein collects no telemetry.

A scheduled job re-checks every entry against each new Levenshtein release and
sets `broken` where needed, so readers can tell a quiet module from a broken
one.

### Governance

`GOVERNANCE` in the catalog repository sets:

- **Maintainers.** The catalog opens with at least two named maintainers who
  commit to reviewing pull requests within a week. If fewer than two remain
  for 90 days, the repository is archived with a notice, and the last
  Levenshtein release's module list keeps working.
- **Namespaces.** First come, first served, for a module that exists and
  builds. Reserving a namespace for a module that doesn't exist yet counts as
  squatting and is refused, following crates.io's policy. The reserved names
  above, and names that impersonate another project, are refused.
- **Transfers.** A namespace moves to a new publisher only with the current
  maintainers' agreement, or when the entry is `broken` and a public issue
  asking for the transfer has gone 90 days without an answer.
- **Removal.** A module that is malicious, or that exfiltrates or damages
  source, is marked `withdrawn` at once, and the catalog publishes an advisory.
  The next Levenshtein release carries the status.
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
directives carry over.

## Alternatives considered

| Option | Why it is not the recommendation |
| --- | --- |
| Compile community rules into `levenshtein-lint` (golangci-lint's model, and this page's first draft) | The prototype crashed on a rule that exports a package fact, even with the rule deselected; a rule can hide core findings; and pinning shared dependencies to the core versions made authors' dependencies hostage to Levenshtein releases |
| Go `.so` plugins | The version skew that led golangci-lint to deprecate them; no Windows support |
| One vet tool per module (`go vet -vettool`, `unitchecker`) | Isolates each module, but every module loads the program again, findings leave `lintcmd`'s JSON, and `//lint:ignore` stops applying. The chosen design keeps one extra load for all community rules together |
| RPC plugins, as in TFLint | Needs a protocol designed and versioned here for something `go/analysis` already defines |
| Declarative rules only (Semgrep, ruleguard) | Lower risk and easy to review, but cannot express type-aware rules like `LV1001` or `LV1006`. They could be a second tier later: `go-ruleguard` bundles are also Go modules |
| A catalog in this repository | Puts every community rule in the main repository's review queue, which is what this proposal exists to avoid |
| Fetching catalog status during checks | Breaks reproducible pinned runs and needs network during checks; a module list shipped in each release does neither |

## Phasing

| Phase | Work | Gate |
| --- | --- | --- |
| 0 | Publish `lvrules-template` from `examples/rule-module` and the `lvrules-check` action, marked unstable (`v0`). Document the contract and the `command` check as the interim way to run rules | None, but a public template is a promise. Settle this page first |
| 1 | `rule_modules` config and validation; the community linter on Dagger; selection, globs, and advisory findings; merged findings with `source` and `url`; the `warnings` list; mixed-directive and failure handling; the lint step without shared caches. The contract becomes stable | Two unrelated requests, from outside this repository and its pilots, to run rules Levenshtein will not ship |
| 2 | Catalog repository with governance, admission CI, scheduled checks, `status` and the shipped module list; the Renovate preset; `./verify rules`; renames | A second, unrelated rule module exists, and the catalog has two maintainers |
| 3 | Generated catalog page and corpus counts; native executor support; private modules; network-less lint step once Dagger supports it | The catalog lists enough entries that browsing `catalog.json` is inconvenient, or a consumer needs native or private modules |

Until the lint step runs without network access, the documentation carries the
private-repository warning from the [security model](#security-model).

## Design review

Two rounds of independent review, each with one reviewer for product and user
experience and one for implementation, found the problems below. This
revision resolves them. Findings marked "verified" were reproduced against a
prototype.

### First round

| Finding | Resolution |
| --- | --- |
| A rule exporting a package fact crashed the combined linter, even deselected (verified) | [Separate process](#running-community-rules) |
| "Off unless selected" still ran every declared rule, so a rule could hide or crash core findings (verified) | Separate process; only rules some check selects are registered |
| The dependency conflict check failed modules that only added dependencies, and kept authors on core versions (verified) | Only the Staticcheck and Go pins are checked |
| Namespaces with digits joined core categories: `sa1_nopanic` matched `SA1*` (verified) | [Lowercase letters only](#rule-names) |
| The pattern expression rejected `_`, so community codes could not be selected (verified) | [Selection](#selection) updates both copies |
| A consumer's `all` would turn on every community rule | `all` means core rules |
| Analyzer errors were swallowed and passed silently (verified) | Rule failures become [check errors](#errors) |
| Result keys cannot include sums resolved inside Dagger | [Keys](#result-keys) use declared entries |
| Import aliases collided with namespaces such as `community` or `type` (verified) | Positional aliases `m0`, `m1`, ... |
| Duplicate names were dropped silently, and `-list-checks` showed blank titles (verified) | Validation at build; a title line for `Doc` |
| Consumer-chosen namespaces made codes mean different things in different repositories | Publisher-owned namespaces; aliases only for collisions |
| Findings did not say where a rule came from or link to its docs | `source` and `url` on every finding |
| No rule settings, advisory mode, upgrade path, rename path, or autofix story | `settings`, [advisory](#advisory-findings), [upgrades](#upgrades), [renames](#renames-deprecation-and-graduation), fixes through `singlechecker -fix` |
| No governance, security reporting, or removal policy for the catalog | [Governance](#governance) and `status` |
| "Verified" suggested an audit | "Builds with Levenshtein vX" |
| `levenshtein` as a package name and topic is hard to find | `lvrules` package, `levenshtein-lint-rules` topic |
| The example reported panics in tests and `main_test`, exempted `Mustard`, and missed package-level code (verified) | Fixed, with fixtures that fail when any exemption is removed |

### Second round

| Finding | Resolution |
| --- | --- |
| The lint step cannot type-check code without module sources, and the pinned Dagger cannot disable networking (verified) | A download step feeds sources as a plain directory; the network gap is stated, with a warning for private repositories, and closing it is a phase 3 item |
| The failure wrapper missed analyzers reached through `Requires`, reported failures inside the go-build cache, and could not catch `os.Exit(0)` (verified) | Clone the whole `Requires` graph; a failure file instead of findings; a completion marker |
| A directive mixing core and community codes was reported as unused by one linter or the other (verified) | `lvrules_mixed` asks for one directive per linter |
| `go mod tidy` could resolve different versions over time for a module with an incomplete `go.mod` (verified) | The build, and `scripts/test-example-rules`, fail on "finding module for package" |
| Advisory findings had no way into a passing result (verified: `-fail` gives severity `warning` and exit 0) | `GoLint` returns findings for passes; job summary and `jq` recipe show them |
| Most "exit 2" errors are only knowable after the build | [Errors](#errors) split by when they can be detected; `namespace` in the entry lets the CLI route patterns |
| Warnings had nowhere to appear, and vanished on cache hits | A `warnings` list stored with results, with every code listed |
| `withdrawn` needed network at run time, was bypassed by the cache, and broke pinned runs | A module list shipped in each release takes effect when the consumer updates |
| One namespace per module clashed with Go major versions and organizations with several modules | Publisher entries with several module paths; topic namespaces |
| Community rules made native `go-lint` checks a configuration error, blocking this repository's own setup | Native checks skip community rules with a warning; `"lint": {"rule_modules": false}` opts one check out |
| The precedence of `select`, `lint.checks`, `advisory`, and `-all` was undefined | An ordered pattern list and a table of effects |
| `Doc` normalization duplicated text in `-explain` (verified) | Only add a title line when there is none |
| Two modules returning one analyzer would share settings | Rejected at build |
| "Your first rule" needed files a template does not have | `gonew` scaffolding and a reusable `lvrules-check` action |
| The example required the exact pinned Go and `x/tools`, used `@latest`, and had no reserved namespace | Go 1.24 and `x/tools` v0.40.0; exact pins; `example` reserved |
| No compatibility policy for the `lvrules` contract, and hand-typed catalog metadata | Optional additive exports; catalog rule lists generated from the module |
| The catalog's `verified` field contradicted its label, `deprecated` also meant "broken", and governance had no named owners or approval for dependency changes | `builds_with`, a separate `broken` status, two named maintainers, and approval for `go.mod` changes |
| The Renovate regex depended on key order (verified) | A JSONata manager |
| The example reported panics in generated files (verified) | Skipped, with a fixture |
| Phase 1's gate was a single request, and the top-level `rules` field collided with the word for one rule | Two unrelated requests; the field is `rule_modules` |
