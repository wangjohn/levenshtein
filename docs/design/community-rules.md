# Community lint rules: design

This is the design behind [community lint rules](../community-rules.md): the problem, how the community linter is built and run, the catalog that is still a proposal, and the phasing. To use a rule module or write one, read the how-to instead.

**Status: phase 1 shipped in v0.2.0**: `rule_modules`, the community linter, selection, advisory findings, merged findings, warnings, and error handling. Phases 2 and 3 (the catalog, `./verify rules`, the Renovate preset, native execution, private modules) are still proposals; see [Phasing](#phasing).

## Problem

A rule Levenshtein does not ship can only run as a native `command` check, which loses shared selection, `//lint:ignore`, JSON findings, and result caching.

## Proposal

- Anyone publishes lint rules as an ordinary Go module.
- A consumer pins that module in `levenshtein.json`.
- The rules run beside the core rules without entering `runner/lint`.
- A separate catalog lists modules that build and have an owner, and good rules graduate into core from there.

The design borrows golangci-lint's module plugins, TFLint's exact pins, and ESLint's plugin-owned rule names.

```text
levenshtein.json ──> build community linter ──> run beside core linter ──> one report
 (module@version)     (pinned Staticcheck)        (separate process)        (source + url per finding)
```

## Running community rules

The core linter is unchanged. Community rules run in the community linter, a separate process on the same pinned Staticcheck. Findings from both are merged into one report. Compiling community rules into the core linter, as golangci-lint does, was prototyped and rejected:

- a rule that exports a package fact crashed the core linter;
- a rule could hide core findings, or change them by raising a shared dependency.

The cost is a second package load, only for checks that use community rules. On this repository's root module that was about 2.5 s, next to 6.5 s for the core linter.

| Piece | Where |
| --- | --- |
| Configuration, load-time checks, planning, withdrawn versions | `internal/verify/rulemodules.go` |
| The community linter's runtime: validation, selection, settings, `lvrules_*` codes, failure guard, report | `runner/community` |
| The builder that generates and compiles the linter | `runner/community/internal/build`, run as `cmd/levenshtein-community-build` |
| The three Dagger containers and the merged report | `runner/community.go`, `GoLint` and `GoLintReport` in `runner/main.go` |
| Versions a release refuses or warns about | `runner/rule-modules.json` |

### Build

`GoLint` takes a `ruleModules` argument, the check's planned modules as JSON, and uses three containers.

1. **Build** (network, shared Go caches): `levenshtein-community-build` generates a module that requires the pinned Staticcheck, `runner/community`, and each `module@version`, runs `go mod tidy` against the checksum database, and builds with `GOTOOLCHAIN=local`. It never sees the consumer's source, so every check with the same pins shares one build. The build fails if:
   - a module's `go` directive is newer than `.go-version`;
   - tidy prints "finding module for package", which means some module imports a package no `go.mod` requires, so the result would change with the proxy;
   - Staticcheck or any rule module resolves off its pin;
   - the module has no `lvrules` package, or it does not declare `Namespace` and `Analyzers`, or a literal `Namespace` differs from `levenshtein.json`;
   - the generated main does not compile.

   It also asks the proxy whether each pin is retracted, which becomes a `rule-module-retracted` warning.
2. **Download** (network): `go mod download` for the consumer's module, checked against its `go.sum`, into a plain directory. It sees only the module's `go.mod`, `go.sum`, `go.work`, and `go.work.sum` files, so editing any other file reuses the download. A vendored module is not downloaded at all: the lint step reads `vendor/`, and its dependencies may be private modules no proxy serves. As Go does, the step looks for `vendor/modules.txt` at the root of the nearest `go.work` at or above the module, and in the module's own directory when there is no workspace.

   Both steps let a failing command fail rather than record its exit code. Dagger never caches a failed command, so a transient failure, such as an unreachable proxy, is retried on the next run instead of being replayed until the pins change. A run with `rerun_checks` repeats both steps, so it also refreshes the retraction check.
3. **Lint** (no shared state): the pinned Go image, which Staticcheck needs because it runs `go list`. It gets the linter binary and the downloaded sources as plain directories, `GOPROXY=off`, a fresh `GOCACHE`, and a Staticcheck cache volume of its own. `GOFLAGS` is cleared rather than set to `-mod=readonly`, so a vendored module still lints from `vendor/`; Go's default is `readonly` otherwise.

The generated `main` imports each module under a positional alias (`m0`, `m1`, ...) and lists nothing else:

```go
// Code generated by levenshtein from levenshtein.json; DO NOT EDIT.
community.Main([]community.Module{
	{Path: "github.com/acme/lvrules-errors", Version: "v1.4.0", Namespace: m0.Namespace, Renamed: m0.Renamed, Analyzers: m0.Analyzers()},
})
```

`Renamed` is `nil` when the module declares none; the builder reads the `lvrules` package's declarations to find out. The check's selection, advisory rules, and settings are not compiled in. The runner passes them at run time with `-lvrules.config`, so one binary serves every check with the same pins, and each run registers only the rules its check selects, because Staticcheck runs every registered analyzer whether or not it is selected.

`runner/community` is a separate module with the same Staticcheck pin as `runner/lint` (a test keeps the two pins and `runner/toolchain.json` equal), and no other connection to it. At startup, the community linter:

- validates each module against [the contract](../community-rules.md#the-contract);
- resolves the check's patterns, advisory entries, and settings, refusing any that match no rule, and warns about old names and deprecated rules;
- rejects an analyzer that two modules share, because they would share its settings;
- renames each selected rule to its code and adds a `Doc` title line only when one is missing;
- wraps every analyzer in each selected rule's `Requires` graph, in place, with the failure guard;
- registers `lvrules_mixed` and `lvrules_renamed`;
- sets `-checks` to the selected codes and `-fail` to the non-advisory ones, and refuses either flag on its own command line;
- gives each distinct set of settings its own Staticcheck cache directory, because settings reach a rule through analyzer flags, which Staticcheck's cache key leaves out;
- hands everything to `lintcmd`, calling `Execute` rather than `Run`, then writes its report to `-lvrules.report`. The report lists each rule's source, URL, and advisory setting, the warnings, and a failure if there was one, and doubles as the completion marker.

A stale directive that names only advisory rules is rewritten to a warning in the JSON output, and the exit code follows: 1 while any finding still fails.

### Failures

Staticcheck's runner swallows an error an analyzer returns: the package passes, and the pass is cached. A panic kills the whole process. The failure guard wraps each selected analyzer and everything it requires, and stops the run at the first error or panic: the community linter writes its report with that failure and exits 4. Staticcheck writes a package's results to its cache only after every analyzer on the package has finished, so a failed package is never cached, wherever the cache lives, and no later or concurrent run can reuse it.

The community linter's guard is generated from the core linter's, which reports a failure on stderr and exits 2. The core linter also registers only the rules a check selects, as the community linter does, so a rule that is turned off never runs and cannot fail the run.

### Results

A passing check can now carry findings, all advisory, and warnings. The CLI calls `GoLintReport`, which returns them as JSON; `GoLint` stays a Dagger check that returns only an error. A failing check carries its findings, warnings, and any community error as error extensions. The merged report drops both linters' "unused directive" reports at a line where `lvrules_mixed` reported. A compile error in the community linter's output makes the check an error; the core linter reports the same error first.

### Result keys

A result key includes the check's planned `rule_modules` entries and the shared implementation, including `runner/community` and `runner/rule-modules.json`. A check without rule modules keeps the key it had before rule modules existed. The key cannot include versions resolved inside Dagger. It does not need to: the build rejects any resolution that depends on the proxy's current state, so the entries determine the resolved versions.

## Upgrades through Renovate

Pins are exact, so upgrades come through Renovate. A preset built on Renovate's [JSONata manager](https://docs.renovatebot.com/modules/manager/jsonata/) finds every `rule_modules` entry whatever order its keys are in. It needs testing against a real repository before phase 2:

```json
{"customManagers": [{"customType": "jsonata", "fileFormat": "json",
  "managerFilePatterns": ["/(^|/)levenshtein\\.json$/"],
  "matchStrings": ["$each(rule_modules, function($entry, $module) { {\"depName\": $module, \"currentValue\": $entry.version} })"],
  "datasourceTemplate": "go"}]}
```

From phase 2, `./verify rules` lists each module's pin, newer versions, retractions, and catalog status.

## The catalog

The catalog lives in its own repository, `levenshtein-lint-rules`, so community rules never enter this repository's review queue. It has its own `README`, `CONTRIBUTING`, `GOVERNANCE`, `SECURITY.md`, `CODE_OF_CONDUCT`, and `CODEOWNERS`. Each publisher has one entry:

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

**Admission.** CI runs on `pull_request` with a read-only token, no secrets, and GitHub-hosted runners. It:

- runs the module's tests;
- builds the module against the current release;
- checks that the namespace matches and is not already taken;
- requires `Doc`, `URL`, at least one finding and one clean case, and an OSI license;
- generates the entry's rule list from the built module;
- runs each rule over a fixed corpus and records its finding count;
- flags findings that land on the same lines as a core rule's, the way [rule selection](../rule-selection.md) vets upstream rules.

A maintainer approves every new namespace, and every update that changes the module's `go.mod`. Other updates merge on green CI, and the catalog page says so.

**The page** shows each publisher's rules, "builds with Levenshtein vX", the dates of the last release and last check, corpus counts, and how many public repositories reference it. The label says "builds with", not "verified": the code has not been audited. A scheduled job re-checks every entry against each new release.

**Governance.**

- **Maintainers.** The catalog needs two named maintainers who review within a week. With fewer than two for 90 days, it is archived.
- **Namespaces.** First come, first served, for modules that already exist. Holding a name for a module that doesn't exist yet counts as squatting, as on crates.io.
- **Transfers.** Only with the owners' agreement, or after 90 unanswered days on a `broken` entry.
- **Removal.** A malicious module is `withdrawn` at once and gets an advisory.

**Discovery.** The catalog page, the GitHub topic `levenshtein-lint-rules`, and `lvrules-<topic>` repository names. Levenshtein collects no telemetry.

## Phasing

| Phase | Work | Gate |
| --- | --- | --- |
| 0 | Publish `lvrules-template` and the `lvrules-check` action as `v0`; document the contract and the `command` check as a stopgap | This spec is settled |
| 1 | `rule_modules`, the community linter, selection, advisory findings, merged findings, `warnings`, error handling. Shipped in v0.2.0 with the contract still `v0`, unstable until [Levenshtein 1.0](../versioning.md#what-10-means) | Two unrelated requests, from outside this repository and its pilots, for rules Levenshtein will not ship |
| 2 | Catalog, governance, admission CI, the shipped module list, the Renovate preset, `./verify rules`, renames | A second unrelated rule module, and two catalog maintainers |
| 3 | Catalog page, corpus counts, native executor, private modules, network-less lint step | Catalog size, or a consumer who needs native or private modules |
