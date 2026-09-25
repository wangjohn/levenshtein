# Community lint rules

**Status: phase 1 is implemented** on the main branch and ships with the next
release: `rule_modules`, the community linter, selection, advisory findings,
merged findings, warnings, and error handling. Phases 2 and 3 (the catalog,
`./verify rules`, the Renovate preset, native execution, private modules) are
still proposals; see [Phasing](#phasing).

**Problem.** A rule Levenshtein does not ship can only run as a native
`command` check, which loses shared selection, `//lint:ignore`, JSON findings,
and result caching.

**Proposal.**

- Anyone publishes lint rules as an ordinary Go module.
- A consumer pins that module in `levenshtein.json`.
- The rules run beside the core rules without entering `runner/lint`.
- A separate catalog lists modules that build and have an owner, and good
  rules graduate into core from there.

The design borrows golangci-lint's module plugins, TFLint's exact pins, and
ESLint's plugin-owned rule names.

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

A module keeps the exports it has published, too. The shared
[`go-apidiff`](checks.md#api-compatibility) check fails a change that removes or
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

1. `gonew github.com/wangjohn/lvrules-template github.com/you/lvrules-errors`
   copies the template and rewrites the module path. Choose a namespace; the
   template's CI fails while it is still `example`. Until the template
   repository is published (phase 0), copy
   [`examples/rule-module`](../examples/rule-module) instead.
2. Write the analyzer and add it to `Analyzers()`.
3. Add fixtures under `testdata/src` with a `// want` comment on every expected
   finding, plus clean cases. Break the rule once to check that the test fails.
4. CI runs the reusable `lvrules-check` action. It builds the module into a
   community linter for each Levenshtein release you list and runs it on your
   fixtures.
5. Tag `v0.1.0` with release notes, name the repository `lvrules-<topic>`, and
   add the `levenshtein-lint-rules` topic.
6. From phase 2, open a pull request to the catalog.

[`examples/rule-module`](../examples/rule-module) is the working template:
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

Each result gains a `warnings` list. It is stored with cached results, so a
cache hit repeats it, and printed in the job summary:

| Warning | When |
| --- | --- |
| `rule-modules-skipped` | A native `go-lint` check skipped community rules |
| `rule-module-deprecated` | This Levenshtein release lists the pinned version as deprecated |
| `rule-module-retracted` | The author retracted the pinned version, detected when the linter is built |
| `rule-renamed` | A pattern, advisory entry, or setting uses a rule's old name |
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

From phase 2, `./verify rules` lists each module's pin, newer versions,
retractions, and catalog status. Checks themselves never contact the catalog, so a pinned run
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

| Piece | Where |
| --- | --- |
| Configuration, load-time checks, planning, withdrawn versions | `internal/verify/rulemodules.go` |
| The community linter's runtime: validation, selection, settings, `lvrules_*` codes, failure guard, report | `runner/community` |
| The builder that generates and compiles the linter | `runner/community/internal/build`, run as `cmd/levenshtein-community-build` |
| The three Dagger containers and the merged report | `runner/community.go`, `GoLint` and `GoLintReport` in `runner/main.go` |
| Versions a release refuses or warns about | `runner/rule-modules.json` |

### Build

`GoLint` takes a `ruleModules` argument, the check's planned modules as JSON,
and uses three containers.

1. **Build** (network, shared Go caches): `levenshtein-community-build`
   generates a module that requires the pinned Staticcheck, `runner/community`,
   and each `module@version`, runs `go mod tidy` against the checksum
   database, and builds with `GOTOOLCHAIN=local`. It never sees the consumer's
   source, so every check with the same pins shares one build. The build fails
   if:
   - a module's `go` directive is newer than `.go-version`;
   - tidy prints "finding module for package", which means some module imports
     a package no `go.mod` requires, so the result would change with the proxy;
   - Staticcheck or any rule module resolves off its pin;
   - the module has no `lvrules` package, or it does not declare `Namespace`
     and `Analyzers`, or a literal `Namespace` differs from `levenshtein.json`;
   - the generated main does not compile.

   It also asks the proxy whether each pin is retracted, which becomes a
   `rule-module-retracted` warning.
2. **Download** (network): `go mod download` for the consumer's module,
   checked against its `go.sum`, into a plain directory. It sees only the
   module's `go.mod`, `go.sum`, `go.work`, and `go.work.sum` files, so editing
   any other file reuses the download. A vendored module is not downloaded at
   all: the lint step reads `vendor/`, and its dependencies may be private
   modules no proxy serves. As Go does, the step looks for
   `vendor/modules.txt` at the root of the nearest `go.work` at or above the
   module, and in the module's own directory when there is no workspace.

   Both steps let a failing command fail rather than record its exit code.
   Dagger never caches a failed command, so a transient failure, such as an
   unreachable proxy, is retried on the next run instead of being replayed
   until the pins change. A run with `rerun_checks` repeats both steps, so it
   also refreshes the retraction check.
3. **Lint** (no shared state): the pinned Go image, which Staticcheck needs
   because it runs `go list`. It gets the linter binary and the downloaded
   sources as plain directories, `GOPROXY=off`, a fresh `GOCACHE`, and a
   Staticcheck cache volume of its own. `GOFLAGS` is cleared rather than set
   to `-mod=readonly`, so a vendored module still lints from `vendor/`; Go's
   default is `readonly` otherwise.

The generated `main` imports each module under a positional alias (`m0`, `m1`,
...) and lists nothing else:

```go
// Code generated by levenshtein from levenshtein.json; DO NOT EDIT.
community.Main([]community.Module{
	{Path: "github.com/acme/lvrules-errors", Version: "v1.4.0", Namespace: m0.Namespace, Renamed: m0.Renamed, Analyzers: m0.Analyzers()},
})
```

`Renamed` is `nil` when the module declares none; the builder reads the
`lvrules` package's declarations to find out. The check's selection,
advisory rules, and settings are not compiled in. The runner passes them at
run time with `-lvrules.config`, so one binary serves every check with the same
pins, and each run registers only the rules its check selects, because
Staticcheck runs every registered analyzer whether or not it is selected.

`runner/community` is a separate module with the same Staticcheck pin as
`runner/lint` (a test keeps the two pins and `runner/toolchain.json` equal),
and no other connection to it. At startup, the community linter:

- validates each module against [the contract](#the-contract);
- resolves the check's patterns, advisory entries, and settings, refusing any
  that match no rule, and warns about old names and deprecated rules;
- rejects an analyzer that two modules share, because they would share its
  settings;
- renames each selected rule to its code and adds a `Doc` title line only
  when one is missing;
- wraps every analyzer in each selected rule's `Requires` graph, in place, with
  the failure guard;
- registers `lvrules_mixed` and `lvrules_renamed`;
- sets `-checks` to the selected codes and `-fail` to the non-advisory ones,
  and refuses either flag on its own command line;
- gives each distinct set of settings its own Staticcheck cache directory,
  because settings reach a rule through analyzer flags, which Staticcheck's
  cache key leaves out;
- hands everything to `lintcmd`, calling `Execute` rather than `Run`, then
  writes its report to `-lvrules.report`. The report lists each rule's source,
  URL, and advisory setting, the warnings, and a failure if there was one,
  and doubles as the completion marker.

A stale directive that names only advisory rules is rewritten to a warning in
the JSON output, and the exit code follows: 1 while any finding still fails.

### Failures

Staticcheck's runner swallows an error an analyzer returns: the package passes,
and the pass is cached. A panic kills the whole process. The failure guard
wraps each selected analyzer and everything it requires, and stops the run at
the first error or panic: the community linter writes its report with that
failure and exits 4. Staticcheck writes a package's results to its cache only
after every analyzer on the package has finished, so a failed package is never
cached, wherever the cache lives, and no later or concurrent run can reuse it.

The core linter carries a copy of the guard; it reports a failure on stderr and
exits 2. It also registers only the rules a check selects, as the community
linter does, so a rule that is turned off never runs and cannot fail the run.

### Errors

| Detected | Examples | Outcome |
| --- | --- | --- |
| Loading `levenshtein.json` | Bad version or namespace syntax; a namespace declared twice; settings keys that differ only in case; a pattern naming an undeclared namespace; a literal advisory rule no go-lint check selects; community patterns on a check that sets `rule_modules` to `false`; a version this release lists as withdrawn | Configuration error, exit 2 |
| Building the community linter | `namespace` differs from the module; an incomplete `go.mod`; Go or Staticcheck above the pins; another module moved a pin; no compile against the pinned dependencies | Check error naming the module, exit 1 |
| Starting the community linter | An unknown rule or setting; two settings keys that reach the same rule; a pattern or advisory entry that matches no rule; a missing `URL`; duplicate names | Check error naming the module, exit 1 |
| Running a rule | The rule returns an error or panics, including in a dependency | Check error such as "errs_nopanic (…@v1.4.0) failed on example.com/app/store: panic: …", exit 1 |
| After the run | No report, for example because a rule called `os.Exit(0)` | Check error, exit 1 |

Core findings are reported in every case. Messages state what is known and
never guess a fix, for example: "…@v1.5.0 requires honnef.co/go/tools v0.9.0
(through example.com/helper@v0.3.0), but this release pins v0.8.1."

### Results

A passing check can now carry findings, all advisory, and warnings. The CLI
calls `GoLintReport`, which returns them as JSON; `GoLint` stays a Dagger
check that returns only an error. A failing check carries its findings,
warnings, and any community error as error extensions. The merged report drops
both linters' "unused directive" reports at a line where `lvrules_mixed`
reported. A compile error in the community linter's output makes the check an
error; the core linter reports the same error first.

### Result keys

A result key includes the check's planned `rule_modules` entries and the shared
implementation, including `runner/community` and `runner/rule-modules.json`. A
check without rule modules keeps the key it had before rule modules existed.
The key cannot include versions resolved inside Dagger. It does not need to:
the build rejects any resolution that depends on the proxy's current state, so
the entries determine the resolved versions.

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
- **Native execution (phase 3) is trusted-only**, like native `command` checks.

Private rule modules wait for phase 3: the build would receive a token as a
Dagger secret, and the entry would carry an `h1:` `sum`.

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
