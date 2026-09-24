# Community lint rules

**Status: proposal.** Only the example module and its CI check exist today.

Today a rule Levenshtein does not ship can only run as a native `command`
check, which loses shared selection, `//lint:ignore`, JSON findings, and result
caching. With this proposal, anyone can publish Go lint rules as an ordinary Go
module. A Levenshtein consumer pins that module in `levenshtein.json`, and the
rules run beside the core rules without ever entering `runner/lint`. A
separate catalog repository lists modules that build and have an owner, and
good rules graduate into the core selection from there. The design follows golangci-lint's module plugins,
TFLint's exact pins, and ESLint's plugin-owned rule names.

```text
levenshtein.json ──> build community linter ──> run beside core linter ──> one report
 (module@version)     (pinned Staticcheck)        (separate process)        (source + url per finding)
```

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
phase 1 ships.

Each analyzer needs:

- **`Name`**: a Go identifier, unique in the module ignoring case.
- **`Doc`**: a one-line summary first, then a blank line if there is more.
- **`URL`**: a stable page (pkg.go.dev, or docs for each release) saying what
  the rule reports, why, how to fix it, and how to suppress it.
- **Originality**: no copy of a core rule. Returning `nilness.Analyzer` would
  report every finding twice.

Facts and suggested fixes are fine. Levenshtein reports findings; the module's
own `singlechecker` wrapper applies fixes with `-fix`.

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

1. `gonew github.com/wangjohn/lvrules-template github.com/you/lvrules-errors`
   copies the template and rewrites the module path. Choose a namespace; the
   template's CI fails while it is still `example`.
2. Write the analyzer and add it to `Analyzers()`.
3. Add fixtures under `testdata/src` with a `// want` comment on every expected
   finding, plus clean cases. Break the rule once to check that the test fails.
4. CI runs the reusable `lvrules-check` action. It builds the module into a
   community linter for each Levenshtein release you list and runs it on your
   fixtures.
5. Tag `v0.1.0` with release notes, name the repository `lvrules-<topic>`, and
   add the `levenshtein-lint-rules` topic.
6. From phase 2, open a pull request to the catalog.

[`examples/rule-module`](../examples/rule-module) is the working template. Its
namespace is `example` and its one rule, `example_nopanic`, reports `panic` in
library code; its README documents the rule. Levenshtein's own CI keeps the
template working. The `example` target in `levenshtein.json` runs lint, vet,
mod, and tests on it. `scripts/test-example-rules` checks that its `go`
directive is not newer than `.go-version`, builds it into a community linter on
the pinned Staticcheck, and runs that linter on a sample package, including a
`//lint:ignore`. That script uses a plain rename for now, and switches to
`runner/community` once that exists.

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
| `advisory` | Patterns for selected rules that report without failing the check; naming a rule no check selects is an error |
| `settings` | String flag values for each rule, set through the analyzer's `Flags` |
| `alias` | Replaces `namespace` in this repository when two modules collide |

To stop using a module, remove its entry. A single check opts out with
`"lint": {"rule_modules": false}`. `rule_modules` is optional and additive to
version 1 of the configuration.

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
| `go-lint/app` | passed | 2 advisory |
  [advisory] store/store.go:7:3 errs_sentinel: compare errors with errors.Is (https://pkg.go.dev/github.com/acme/lvrules-errors/sentinel)
```

A stale `//lint:ignore` is advisory only if every code it names is advisory. The
README's `jq` recipe for reading reports gains an `[advisory]` prefix and the
`url`.

### Ignore directives

`//lint:ignore` and `//lint:file-ignore` work as today, as long as each directive
names only core codes or only community codes. Each linter checks only its own
directives for staleness. A directive that mixes the two, such as
`//lint:ignore SA4006,errs_nopanic`, is reported as `lvrules_mixed`, asking for
one directive per linter on consecutive lines. The runner drops both linters'
"unused directive" reports at that line.

### Findings and warnings

Every finding gains `source`, `url`, and `advisory`. Core findings get `url` and
`advisory` too.

```json
{"code": "errs_nopanic", "message": "Load panics; return an error so callers can handle the failure",
 "location": {"file": "store/store.go", "line": 7, "column": 3},
 "source": "github.com/acme/lvrules-errors@v1.4.0",
 "url": "https://pkg.go.dev/github.com/acme/lvrules-errors/nopanic", "advisory": false}
```

Each result gains a `warnings` list. It is stored with cached results and
printed in the job summary:

| Warning | When |
| --- | --- |
| `rule-modules-skipped` | A native `go-lint` check skipped community rules |
| `rule-module-deprecated` | This Levenshtein release lists the pinned version as deprecated |
| `rule-module-retracted` | The author retracted the pinned version, detected when the linter is built |
| `rule-renamed` | A pattern uses a rule's old name |
| `rule-deprecated` | A selected rule is deprecated |

### Executors

Community rules run only on the Dagger executor until phase 3. A native
`go-lint` check runs its core rules and warns that it skipped community rules,
so a configuration can mix executors. `go-http` and `go-sql` never run
community rules.

### Upgrades and withdrawals

Pins are exact, so upgrades come through Renovate. A preset built on Renovate's
[JSONata manager](https://docs.renovatebot.com/modules/manager/jsonata/) finds
every `rule_modules` entry whatever order its keys are in. It needs testing
against a real repository before phase 2:

```json
{"customManagers": [{"customType": "jsonata", "fileFormat": "json",
  "managerFilePatterns": ["/(^|/)levenshtein\\.json$/"],
  "matchStrings": ["$each(rule_modules, function($entry, $module) { {\"depName\": $module, \"currentValue\": $entry.version} })"],
  "datasourceTemplate": "go"}]}
```

`./verify rules` lists each module's pin, newer versions, retractions, and
catalog status. Checks themselves never contact the catalog, so a pinned run
always gives the same answer. Instead, each Levenshtein release ships
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

The core linter is unchanged. Community rules run in the community linter, a
separate process on the same pinned Staticcheck. Findings from both are merged
into one report. Compiling community rules into the core linter, as
golangci-lint does, was prototyped and rejected:

- a rule that exports a package fact crashed the core linter;
- a rule could hide core findings, or change them by raising a shared
  dependency.

The cost is a second package load, only for checks that use community rules.
On this repository's root module that was about 2.5 s, next to 6.5 s for the
core linter.

### Build

The `GoLint` Dagger function gains a `rule_modules` argument and uses three
containers:

1. **Build** (network): generate a module that requires the pinned Staticcheck
   and each `module@version`, run `go mod tidy` against the checksum database,
   and build with `GOTOOLCHAIN=local`. The build fails if:
   - Staticcheck moves off its pin;
   - a module needs a newer Go;
   - tidy prints "finding module for package", which means some module imports
     a package no `go.mod` requires, so the result would change with the proxy.
2. **Download** (network): `go mod download` for the consumer's module, checked
   against its `go.sum`.
3. **Lint** (no shared state): the pinned Go image, which Staticcheck needs
   because it runs `go list`. It gets the linter binary and the downloaded
   sources as plain directories, `GOPROXY=off`, `GOFLAGS=-mod=readonly`, a fresh
   `GOCACHE`, and its own Staticcheck cache volume.

The generated `main` imports each module under a positional alias and lists
only the rules that some check selects. Staticcheck runs every registered
analyzer, selected or not.

```go
// Code generated by levenshtein from levenshtein.json; DO NOT EDIT.
var modules = []community.Module{{
	Path:      "github.com/acme/lvrules-errors",
	Version:   "v1.4.0",
	Namespace: m0.Namespace,
	Renamed:   m0.Renamed, // nil when the module has no Renamed
	Analyzers: m0.Analyzers(),
	Selected:  []string{"nopanic", "sentinel"},
	Settings:  map[string]map[string]string{"nopanic": {"allow": "Must,Should"}},
}}
```

`runner/community` is a new module with the same Staticcheck pin as
`runner/lint`, and no other connection to it. It:

- validates each module;
- clones every analyzer together with the whole `Requires` graph under it, so
  renaming, settings, and the failure wrapper reach dependencies too;
- rejects an analyzer that two modules share, because they would share its
  settings;
- adds a `Doc` title line only when one is missing;
- registers `lvrules_mixed` and `lvrules_renamed`;
- hands everything to `lintcmd`, calling `Execute` rather than `Run` so it can
  write a completion marker.

The community linter runs with `-fail` naming only non-advisory rules, so
advisory findings come back as warnings with exit code 0. `GoLint` returns
findings for passing results too, as `go-mutation` does, and today's rule that
exit 0 means no findings changes to match. The runner reports `compile`
diagnostics once, and accepts the `staticcheck` and `compile` codes from both
linters.

### Errors

| Detected | Examples | Outcome |
| --- | --- | --- |
| Loading `levenshtein.json` | Bad version or namespace syntax; a namespace declared twice; a pattern naming an undeclared namespace; a version this release lists as withdrawn | Configuration error, exit 2 |
| Building the community linter | `namespace` differs from the module; an unknown rule or setting; a missing `URL`; duplicate names; an incomplete `go.mod`; Go or Staticcheck above the pins; no compile against the pinned dependencies | Check error naming the module, exit 1 |
| Running a rule | The rule returns an error or panics, including in a dependency | Written to a failure file; check error such as "errs_nopanic (…@v1.4.0) failed on 3 packages", exit 1 |
| After the run | No completion marker, for example because a rule called `os.Exit(0)` | Check error, exit 1 |

Core findings are reported in every case. Messages state only what is known,
for example: "github.com/acme/lvrules-errors@v1.5.0 requires honnef.co/go/tools
v0.9.0 (through example.com/helper@v0.3.0), but this release pins v0.8.1. Use a
version of the module that supports v0.8.1, or a Levenshtein release that pins
v0.9.0 or later." Staticcheck's runner also swallows errors from core
analyzers, and the core linter needs the same fix.

### Result keys

A result key includes the `rule_modules` entries and the shared implementation,
including `runner/rule-modules.json`. It cannot include versions resolved
inside Dagger. It does not need to: the build rejects any resolution that
depends on the proxy's current state, so the entries determine the resolved
versions.

### Security model

- **Nothing shared is writable.** The lint step mounts no module, build, or
  core Staticcheck cache, so a rule cannot poison other checks or consumers.
- **No credentials** reach the lint step. Only the binary and sources are
  copied in.
- **Pins cannot move.** An exact version checked against the checksum database
  means a push or a replaced tag cannot change what runs.
- **Core findings stay honest.** They come from another process.
- **The network is still reachable.** Dagger v0.21.9 has no option to disable
  networking for one command. `unshare --net` would need
  `InsecureRootCapabilities`, which grants more than it removes. Until Dagger
  offers one, the private-repository [warning](#security) stands.
- **Native execution (phase 3) is trusted-only**, like native `command` checks.

`SECURITY.md` will say that vulnerabilities in a rule go to its module's
maintainers, and a malicious module goes to the catalog. Private rule modules
wait for phase 3: the build would receive a token as a Dagger secret, and the
entry would carry an `h1:` `sum`.

## The catalog

The catalog lives in its own repository, `levenshtein-lint-rules`, so community
rules never enter this repository's review queue. It has its own `README`,
`CONTRIBUTING`, `GOVERNANCE`, `SECURITY.md`, `CODE_OF_CONDUCT`, and
`CODEOWNERS`. Each publisher has one entry:

```json
{"namespace": "errs",
 "modules": ["github.com/acme/lvrules-errors", "github.com/acme/lvrules-errors/v2"],
 "description": "Error handling rules for library packages", "license": "MIT",
 "maintainers": ["@acme-dev", "@acme-ops"],
 "builds_with": {"module": "github.com/acme/lvrules-errors/v2", "version": "v2.0.1", "levenshtein": "v0.3.0", "checked": "2026-10-01"},
 "status": "active", "replacement": null}
```

| `status` | Meaning | Effect on consumers |
| --- | --- | --- |
| `active` | Builds with the current release and has an owner | None |
| `broken` | Has not built for 90 days; set and cleared automatically | Shown on the catalog page |
| `deprecated` | Superseded by `replacement` | Warning, after their next Levenshtein update |
| `withdrawn` | Malicious or dangerous | Configuration error, after their next Levenshtein update |

**Admission.** CI runs on `pull_request` with a read-only token, no secrets,
and GitHub-hosted runners. It:

- runs the module's tests;
- builds the module against the current release;
- checks that the namespace matches and is not already taken;
- requires `Doc`, `URL`, at least one finding and one clean case, and an OSI
  license;
- generates the entry's rule list from the built module;
- runs each rule over a fixed corpus and records its finding count;
- flags findings that land on the same lines as a core rule's, the way
  `docs/checks.md` vets upstream rules.

A maintainer approves every new namespace, and every update that changes the
module's `go.mod`. Other updates merge on green CI, and the catalog page says
so.

**The page** shows each publisher's rules, "builds with Levenshtein vX", the
dates of the last release and last check, corpus counts, and how many public
repositories reference it. The label says "builds with", not "verified": the
code has not been audited. A scheduled job re-checks every entry against each
new release.

**Governance.**

- **Maintainers.** The catalog needs two named maintainers who review within a
  week. With fewer than two for 90 days, it is archived.
- **Namespaces.** First come, first served, for modules that already exist.
  Holding a name for a module that doesn't exist yet counts as squatting, as on
  crates.io.
- **Transfers.** Only with the owners' agreement, or after 90 unanswered days
  on a `broken` entry.
- **Removal.** A malicious module is `withdrawn` at once and gets an advisory.

**Discovery.** The catalog page, the GitHub topic `levenshtein-lint-rules`, and
`lvrules-<topic>` repository names. Levenshtein collects no telemetry.

## Phasing

| Phase | Work | Gate |
| --- | --- | --- |
| 0 | Publish `lvrules-template` and the `lvrules-check` action as `v0`; document the contract and the `command` check as a stopgap | This spec is settled |
| 1 | `rule_modules`, the community linter, selection, advisory findings, merged findings, `warnings`, error handling; the contract becomes stable | Two unrelated requests, from outside this repository and its pilots, for rules Levenshtein will not ship |
| 2 | Catalog, governance, admission CI, the shipped module list, the Renovate preset, `./verify rules`, renames | A second unrelated rule module, and two catalog maintainers |
| 3 | Catalog page, corpus counts, native executor, private modules, network-less lint step | Catalog size, or a consumer who needs native or private modules |
