# Community lint rules

A rule Levenshtein does not ship can come from a rule module: an ordinary Go
module of `go/analysis` analyzers that anyone publishes and a repository pins
in `levenshtein.json`. Its rules run beside the [shipped rules](rules.md) in
every `go-lint` check, in a separate process on the same pinned Staticcheck,
and report into the same results, with `//lint:ignore`, selection patterns,
and result caching working as they do for the shipped rules.

This page is the how-to, for [rule authors](#for-rule-authors) and
[consumers](#for-consumers). Rule modules shipped in v0.2.0; the catalog of
published modules, `./verify rules`, native execution, and private modules are
still proposals. The [design](design/community-rules.md) covers the problem,
how the community linter is built, the catalog, and the phasing.

| Term | Meaning |
| --- | --- |
| Rule | One `analysis.Analyzer`, reported under one **code** such as `SA4006` or `errs_nopanic` |
| Rule module | A Go module with an `lvrules` package |
| Namespace | The prefix on every code from one publisher, such as `errs` |
| Core linter | `levenshtein-lint`, built from `runner/lint` |
| Community linter | `levenshtein-community-lint`, built per configuration from the selected rule modules |

## For rule authors

### The contract

```go
package lvrules // in github.com/acme/lvrules-errors/lvrules

const Namespace = "errs"                            // required
func Analyzers() []*analysis.Analyzer                // required
var Renamed = map[string]string{"panics": "nopanic"} // optional: old name -> new name
```

The module never imports Levenshtein; rules are plain `go/analysis` analyzers,
so they also work in golangci-lint, nogo, and `singlechecker`. The contract only
grows by optional exports. A breaking change would get a new package name, with
both supported for at least a year. The contract is unstable (`v0`) until
Levenshtein 1.0 ([versioning](versioning.md#what-10-means)).

A module keeps the exports it has published, too. The shared
[`go-apidiff`](check-kinds-guide.md#api-compatibility) check fails a change that removes or
alters one, and Levenshtein runs it over `examples/rule-module`.

Each analyzer needs:

- **`Name`**: a Go identifier, unique in the module ignoring case.
- **`Doc`**: a one-line summary first, then a blank line if there is more.
- **`URL`**: a stable page (pkg.go.dev, or docs for each release) saying what
  the rule reports, why, how to fix it, and how to suppress it.
- **Originality**: no copy of a core rule. Returning `nilness.Analyzer` would
  report every finding twice.

Facts and suggested fixes are fine. Levenshtein reports findings; the module's
own `singlechecker` wrapper applies fixes with `-fix`.

Community rules report the way core rules do. Every finding reports under the
rule's code, whatever `Category` the analyzer gives it. Findings in generated
files, those with a `// Code generated ... DO NOT EDIT.` header, are dropped;
the rule still runs on them, so the facts it exports stay correct.

The module must build with the Go in `.go-version` and the Staticcheck in
`runner/toolchain.json`. It may require newer versions of other dependencies,
such as `golang.org/x/tools`, because community rules run in their own process.
Require the lowest versions your rules need, so older Levenshtein releases can
use the module too.

### Rule names

A rule reports as `<namespace>_<name>`, such as `errs_nopanic`. That code
appears in findings, patterns, and `//lint:ignore errs_nopanic <reason>`.

- **The publisher owns the namespace**, across all its module paths, including
  `/v2` and later. `errs_nopanic` means the same rule in every repository.
- **Name it after the topic** (`errs`, `sqlsafe`), not the organization, so one
  organization can publish several modules.
- **Lowercase letters only.** Staticcheck treats `SA1*` as "codes whose part
  before the first digit is `SA`", ignoring case. A namespace such as `sa1`
  would fall into that category. With letters only, plus a `_` that no core
  code uses, no core pattern can match a community code.
- **Reserved:** `lvrules`, `example`, `levenshtein`, and `lv`.
- **One module per namespace in a configuration.** Two modules that claim the
  same namespace cannot be used together. The catalog prevents this for listed
  modules.

### Renames, deprecation, and graduation

- **Renaming** a rule and listing the old name in `Renamed` is a minor version.
  Removing a rule, or renaming one without `Renamed`, is a major version.
  Directives that use the old name stop suppressing anything, so the rule
  `lvrules_renamed` reports each one: "`errs_panics` is now `errs_nopanic`;
  update this directive".
- **Deprecating** a rule means adding a `Deprecated:` paragraph to its `Doc`.
  Every check that selects it then gets a warning.
- **Graduating** a rule into core requires users in several unrelated
  repositories, fixtures, and no overlap with a core rule, which is the
  existing bar for new core rules. Core keeps reporting directives that use the
  old code, naming the new `LV` code, for at least six months and two releases.

### Getting started

1. Copy [`examples/rule-module`](https://github.com/wangjohn/levenshtein/tree/main/examples/rule-module), rewrite its module
   path, and choose a namespace; Levenshtein refuses `example` for any other
   module. A published `lvrules-template` repository is planned (phase 0, not
   yet done). Once it exists,
   `gonew github.com/wangjohn/lvrules-template github.com/you/lvrules-errors`
   will copy it and rewrite the module path, and its CI will fail while the
   namespace is still `example`.
2. Write the analyzer and add it to `Analyzers()`.
3. Add fixtures under `testdata/src` with a `// want` comment on every expected
   finding, plus clean cases. Break the rule once to check that the test fails.
4. Run the tests in your own CI. A reusable `lvrules-check` action is planned
   (phase 0, not yet done): it will build the module into a community linter
   for each Levenshtein release you list and run it on your fixtures. Until
   then, `scripts/test-example-rules` shows how this repository does that for
   the example module.
5. Tag `v0.1.0` with release notes, name the repository `lvrules-<topic>`, and
   add the `levenshtein-lint-rules` topic.
6. Once the [catalog](design/community-rules.md#the-catalog) exists (phase 2),
   open a pull request to it.

[`examples/rule-module`](https://github.com/wangjohn/levenshtein/tree/main/examples/rule-module) is the working template:
one rule, `example_nopanic`, documented in its README. This repository's CI
keeps it working:

- The `example` target in `levenshtein.json` gets lint, vet, mod, and tests.
- `scripts/test-example-rules` checks its `go` directive against
  `.go-version`, builds it into a community linter with the runner's own
  builder, and runs that linter on a sample package, including a
  `//lint:ignore`.

## For consumers

### Configuration

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
| `version` | Required. An exact tag or pseudo-version; branches and `latest` are errors |
| `namespace` | Required. Repeats the module's `Namespace`, so patterns can be checked before anything builds |
| `select` | Required, nonempty. Patterns for the rules every `go-lint` check runs from this module; only this module's namespace |
| `advisory` | Patterns for selected rules that report without failing the check. A rule named here that no `go-lint` check selects is a configuration error, and a pattern that matches no rule is a check error |
| `settings` | String flag values for each rule, set through the analyzer's `Flags` |

To stop using a module, remove its entry. A single check opts out with
`"lint": {"rule_modules": false}`. `rule_modules` is optional and additive to
version 1 of the configuration and is supported in version 2.

### Selection

Each `go-lint` check builds one pattern list, and the last pattern that matches
a rule wins, as today:

1. The shipped core selection.
2. `-` for every community rule, so nothing is on by default.
3. Each module's `select`.
4. The check's own `lint.checks`.

| Pattern | Effect |
| --- | --- |
| `all`, `*`, `-all`, `-*` | Core rules only. Adding a module never changes what `all` means |
| `errs_*` | The whole `errs` namespace |
| `errs_no*` | `errs` rules starting with `no`: a pattern containing `_` and ending in `*` is a literal prefix |
| `-errs_nopanic` | Off for this check |
| `other_*` | Configuration error: no declared module uses `other` |

Patterns containing `_` go to the community linter, which expands them into
exact codes; the rest go to the core linter. Matching ignores case.

### Advisory findings

Advisory findings appear in the report and the job summary but never fail the
check:

```text
| `go-lint/app` | passed | 1 advisory |

#### Advisory findings

- [advisory] `go-lint/app` store/store.go:7:3 errs_sentinel: compare errors with errors.Is ([docs](https://pkg.go.dev/github.com/acme/lvrules-errors/sentinel))
```

A stale `//lint:ignore` is advisory only if every code it names is advisory.

### Ignore directives

`//lint:ignore` and `//lint:file-ignore` work as today, as long as each directive
names only core codes or only community codes. Each linter checks only its own
directives for staleness. A directive that mixes the two, such as
`//lint:ignore SA4006,errs_nopanic`, is reported as `lvrules_mixed`, asking for
one directive per linter on consecutive lines. The runner drops both linters'
"unused directive" reports at that line. It drops the core linter's report
only after reading the directive from the source and confirming that it mixes
the two, so a rule module cannot hide a core finding by printing its own
`lvrules_mixed` report.

### Findings and warnings

Every finding gains `source`, `url`, and `advisory`. Core findings get `url` and
`advisory` too.

```json
{"code": "errs_nopanic", "message": "Load panics; return an error so callers can handle the failure",
 "location": {"file": "store/store.go", "line": 7, "column": 3},
 "source": "github.com/acme/lvrules-errors@v1.4.0",
 "url": "https://pkg.go.dev/github.com/acme/lvrules-errors/nopanic", "advisory": false}
```

Each result gains a `warnings` list. It is stored with cached results, so a
cache hit repeats it, and printed in the job summary:

| Warning | When |
| --- | --- |
| `rule-modules-skipped` | A version 1 native `go-lint` check skipped community rules; the message explains migration to version 2 |
| `rule-module-deprecated` | This Levenshtein release lists the pinned version as deprecated |
| `rule-module-retracted` | The author retracted the pinned version, detected when the linter is built |
| `rule-renamed` | A pattern, advisory entry, or setting uses a rule's old name |
| `rule-deprecated` | A selected rule is deprecated |

### Executors

Community rules run only on the Dagger executor until native execution
([phase 3](design/community-rules.md#phasing)). In configuration version 1, a native `go-lint` check runs its core rules and warns that it skipped community rules, preserving existing mixed executor configurations.

Configuration version 2 is available starting with v0.3.0. It rejects a selected native `go-lint` check that participates in configured modules during planning, before any executor or cached verdict is used. Use a Dagger environment for community policy or explicitly set `"lint": {"rule_modules": false}` for an intentional core-only native check. This opt-out already exists in version 1; no extra acknowledgement field is needed. Unselected checks do not trigger this completeness error, and native checks without configured modules remain valid. A participating module is checked even if its selection patterns ultimately select no analyzer: planning does not load modules to resolve patterns. `go-http` and `go-sql` never run community rules.

The [configuration guide](configuration.md#community-rule-modules) describes migration, cached warning wording, and unchanged report and baseline versions.

### Upgrades and withdrawals

Pins are exact, so an upgrade is a change to the `version` in
`levenshtein.json`. A Renovate preset that proposes those changes is
[designed](design/community-rules.md#upgrades-through-renovate) but not yet
tested.

Checks never contact the catalog, so a pinned run always gives the same
answer. Instead, each Levenshtein release ships
`runner/rule-modules.json`, a snapshot of the versions the catalog has marked
`withdrawn` or `deprecated`. When a consumer updates their Levenshtein pin, a
withdrawn version becomes a configuration error and a deprecated one a warning.

### Security

> [!WARNING]
> A community rule runs code that can read your source. Until the lint step
> runs without network access (see [below](#security-model)), enable a
> community rule on a private repository only if you would run its author's
> code against that repository yourself.

## Running community rules

Each `go-lint` check that uses rule modules builds a community linter from its
pinned modules, on the same pinned Staticcheck as the core linter, and runs it
in a separate process beside the core linter; the findings of both are merged
into one report. The build fetches the modules through the module proxy and
checksum database, so a run needs network access the first time a set of pins
is built. The [design](design/community-rules.md#running-community-rules)
explains why the rules run in their own process and how each step works.

### Build

Moved to the [design](design/community-rules.md#build).

### Failures

A rule that returns an error or panics makes the check an error, and nothing
from that run is cached; see [errors](#errors) below. The
[design](design/community-rules.md#failures) explains the failure guard.

### Errors

| Detected | Examples | Outcome |
| --- | --- | --- |
| Loading `levenshtein.json` | Bad version or namespace syntax; a namespace declared twice; settings keys that differ only in case; a pattern naming an undeclared namespace; a literal advisory rule no go-lint check selects; community patterns on a check that sets `rule_modules` to `false`; a version this release lists as withdrawn | Configuration error, exit 2 |
| Planning the selected run | Version 2 native `go-lint` participates in configured rule modules without `lint.rule_modules=false` | Configuration error with migration guidance, exit 2 |
| Building the community linter | `namespace` differs from the module; an incomplete `go.mod`; Go or Staticcheck above the pins; another module moved a pin; no compile against the pinned dependencies | Check error naming the module, exit 1 |
| Starting the community linter | An unknown rule or setting; two settings keys that reach the same rule; a pattern or advisory entry that matches no rule; a missing `URL`; duplicate names | Check error naming the module, exit 1 |
| Running a rule | The rule returns an error or panics, including in a dependency | Check error such as "errs_nopanic (…@v1.4.0) failed on example.com/app/store: panic: …", exit 1 |
| After the run | No report, for example because a rule called `os.Exit(0)` | Check error, exit 1 |

Core findings are reported when execution reaches the core linter. Messages state what is known and
never guess a fix, for example: "…@v1.5.0 requires honnef.co/go/tools v0.9.0
(through example.com/helper@v0.3.0), but this release pins v0.8.1."

### Results

Moved to the [design](design/community-rules.md#results).

### Result keys

A check's result key includes its planned `rule_modules` entries, so moving a
pin re-runs the check. The [design](design/community-rules.md#result-keys) has
the rest.

### Security model

- **Nothing shared is writable.** The lint step mounts no module, build, or
  core Staticcheck cache, so a rule cannot poison other checks or consumers.
  Its own Staticcheck cache volume is named by the linter binary's digest and
  mounted privately, so only the code that would run in that step anyway can
  write to it, and never two steps at once.
- **No credentials** reach the lint step. Only the binary, the dependency
  sources, the consumer's source, and the check's configuration are copied in.
- **Pins cannot move.** An exact version checked against the checksum database
  means a push or a replaced tag cannot change what runs.
- **Core findings stay honest.** They come from another process.
- **The network is still reachable.** Dagger v0.21.9 has no option to disable
  networking for one command. `unshare --net` would need
  `InsecureRootCapabilities`, which grants more than it removes. Until Dagger
  offers one, the private-repository [warning](#security) stands.
- **Native execution ([phase 3](design/community-rules.md#phasing)) is
  trusted-only**, like native `command` checks.

Private rule modules wait for phase 3: the build would receive a token as a
Dagger secret, and the entry would carry an `h1:` `sum`.

## The catalog

The catalog of published rule modules is a proposal; see the
[design](design/community-rules.md#the-catalog).

## Phasing

Moved to the [design](design/community-rules.md#phasing).
