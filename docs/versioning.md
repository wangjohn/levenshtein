# Versioning

Levenshtein is released as `vX.Y.Z` tags ([releases](releases.md)), and a consumer pins one tag or its commit SHA. The [changelog](../CHANGELOG.md) lists what each release changes. This page says which parts of Levenshtein are versioned interfaces, what counts as a breaking change, and what to expect when you move your pin.

The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before 1.0, the minor number plays the role of the major one: a `0.Y.0` release may break things, and a `0.Y.Z` patch release does not.

## What is versioned

| Surface | Where it is defined | Version marker |
| --- | --- | --- |
| Configuration schema | `levenshtein.json`, [configuration](configuration.md) | `"version": 1` or `2` (version 2 is unreleased). Any other value is rejected. Unknown fields are rejected too, so a file that uses a newer field fails on an older release |
| JSON report | The `json` output format, [results](configuration.md#results) | The report's `version`, currently 1. `--render` refuses any other |
| Baseline file | [The baseline file](configuration.md#the-file) | Its `version`, currently 1 |
| CLI | Flags, the run argument, and exit codes, [CLI reference](reference/cli.md) | The release |
| GitHub Action | `action.yml` inputs and outputs, [GitHub Actions](consumer-ci.md#github-actions) | The release |
| Community rule contract | The `lvrules` package a rule module exports, [the contract](community-rules.md#the-contract) | Unstable (`v0`) for now; see that page |
| Rule set | Which rules `go-lint` runs by default, and which checks the default runs include, [rules](rules.md) and [check kinds](check-kinds.md) | The release |
| Tool versions | Go, the container image, Staticcheck, and every other pinned tool, `runner/toolchain.json` and the tool modules under `runner/tools` | The release |

Anything else, such as the text of a finding's message, the Dagger module's functions, the layout of the cache directory, and the Go packages under `internal/`, can change in any release.

## What counts as breaking

The configuration schema grows by optional fields. A field added to version 1 means nothing in a file that does not use it, so existing files keep their meaning and their cached results; `lint`, `rule_modules`, `baseline`, and the `imports` and `apidiff` objects were all added this way. A change that would give an existing file a different meaning, or reject one that used to be accepted, needs a new schema version. Version 2 (unreleased) retains the version 1 shape and requires selected native `go-lint` checks with configured community modules to explicitly opt out with `lint.rule_modules=false`, or move to Dagger. Version 1 keeps its accepted skip-with-warning behavior, so existing files are not silently reinterpreted. JSON report and baseline versions remain 1.

The other surfaces follow one rule before 1.0: a patch release never adds a finding on code that has not changed. It holds only fixes that cannot make a passing repository fail, and documentation. Anything that can, including a newer Staticcheck or gitleaks, moves the minor number.

| Change | Release | What the changelog says |
| --- | --- | --- |
| A new rule or check in a default run, or a default rule turned stricter | Minor | An upgrade note naming the new findings and how to stage them |
| A new optional configuration field, flag, action input, or output | Minor | An entry under Added |
| A removed or renamed flag, action input, output, or configuration field | Minor, after a release that warns about it | A deprecation entry one release ahead, then the removal |
| A new required field, or an exit code that means something new | Minor | An upgrade note |
| A bug fix that turns a silent pass into a finding | Minor | An entry under Fixed that says findings may appear |
| A tool version bump | Minor | An entry under Changed, and an upgrade note when it adds findings |
| A fix that cannot add a finding, or a documentation change | Patch | An entry under Fixed |

A removal or rename gets one minor release of warning first. The release before it lists the change under Deprecated in the changelog, and the removal lands in a later minor release. The same window applies to a rule Levenshtein ships: it is deprecated in one minor release and renamed or removed in a later one. A [community rule module](community-rules.md#renames-deprecation-and-graduation) can go further and keep answering to an old name through `Renamed`, which reports a `rule-renamed` warning.

## Supported releases

Before 1.0, only the newest release is supported. A fix, security fixes included, lands on `main` and ships in the next release, as a patch on the newest minor when it cannot add findings; it is not backported to older releases. Move your pin to get it.

## What 1.0 means

1.0 is the release after which these stop changing in incompatible ways without a major version: supported configuration schema versions, JSON report version 1, the baseline file format, the CLI's flags and exit codes, the GitHub Action's inputs and outputs, and the community rule contract, which leaves `v0` then. The rule set and tool versions keep moving in minor releases after 1.0, under the rules above.

## When you bump your pin

A newer Levenshtein can report findings the old one did not: new analyzers, a new check in a default run (such as `go-mod`, which 0.2.0 added to the defaults of repositories without a `levenshtein.json`), or a newer Staticcheck. Read the changelog entries between your old and new versions, especially the upgrade notes, then choose how to adopt them:

- **Fix them in the same pull request** that moves the pin, when there are few.
- **Record them in a [baseline](configuration.md#baseline)** with `verify main --write-baseline`, so the pin moves now and the findings are fixed later. New findings still fail, and the file only shrinks.
- **Stage a rule** by turning it off for now with a [`lint.checks`](configuration.md#lint-selection) pattern such as `"-unparam"`, and removing the pattern once the code is clean.
- **Leave a check out** of your runs until you are ready, by listing your runs in `levenshtein.json` rather than relying on the defaults.

Each result's cache key covers the shared checks' implementation, so after a bump that changes them every check executes again rather than reusing a result from the old pin.
