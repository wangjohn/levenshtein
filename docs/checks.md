# Lint and CI/CD rules index

Levenshtein verifies a repository with checks, and one of those checks, `go-lint`, is made of rules.

- A **check** is one entry in `levenshtein.json`: a [kind](check-kinds.md), such as `go-lint`, `go-vet`, or `secrets`, run over a target in an environment. Most kinds wrap one pinned tool, such as `go vet`, ShellCheck, or gitleaks, and report what it reports.
- A **rule** is one analyzer inside `go-lint`, reported under its own code, such as `SA4006`, `errcheck`, or `LV1001`. A lint finding's `url` leads to its rule's documentation.

Every check kind runs the same pinned tools locally and in any CI provider, and consumer repositories pick up a change by updating their pinned Levenshtein revision. How a check gets its tools depends on its environment's executor. On `dagger`, the check runs in a container that carries the pinned Go toolchain and tools, so the host needs only a Docker-compatible runtime. On `native`, the check runs on the host: it needs the Go version in `.go-version` on `PATH`, and Levenshtein builds or downloads the other pinned tools itself, checking each download against its pinned SHA-256. [Setup](setup.md#prerequisites) and [native Go checks](configuration.md#native-go-checks) have the details.

## Shared lint and verification checks

Use this table to find the policy behind a finding or choose checks for a
consumer repository. [Check kinds](check-kinds.md) is the complete, generated
inventory, including executors, caching, baseline support, and default runs.
Only `go-lint`, `go-vet`, and `go-mod` run in the default `branch` and
`pre-merge` runs; `main` adds `go-vuln`. Configure the other checks explicitly
when you want them in those runs.

| Area | Where the rules are documented |
| --- | --- |
| Go lint | [Every enabled analyzer and its settings](rules.md#rules-on-by-default), including Staticcheck, go-critic, modernize, and the LV rules |
| Rule selection | [Evidence and analyzers left off](rule-selection.md); [optional complexity](rules.md#opt-in-complexity-gocognit) and [defer-in-loop](rules.md#opt-in-resources-deferinloop) rules |
| Exceptions and additional rules | [Suppressions](rules.md#suppressing-a-finding), [baselines](configuration.md#baseline), and [community rule modules](community-rules.md) |
| Vet, modules, and tests | [All check kinds](check-kinds.md) for `go-vet`, `go-http`, and `go-sql`; [module manifests](check-kinds-guide.md#module-manifests) and [race tests](check-kinds-guide.md#tests) |
| Import boundaries and generated code | [Import rules](check-kinds-guide.md#import-boundaries) and [generated-file freshness](check-kinds-guide.md#generated-code) |
| API and test quality | [API compatibility](check-kinds-guide.md#api-compatibility), [mutation testing](mutation.md), and [experimental semantic lint](semantic-lint.md) (advisory, through an external paid service) |
| GitHub Actions | [Check kinds](check-kinds.md) for actionlint syntax checks; [workflow security](check-kinds-guide.md#workflow-security) for zizmor's offline audits |
| Shell scripts and secrets | [ShellCheck](check-kinds-guide.md#shell-scripts) and [gitleaks](check-kinds-guide.md#secrets) |
| Vulnerabilities | [Check kinds](check-kinds.md) for reachable Go vulnerabilities with govulncheck; [non-Go lockfiles](check-kinds-guide.md#dependency-vulnerabilities) with osv-scanner |
| Custom checks and CI runs | [Configuration](configuration.md), [check kinds guide](check-kinds-guide.md), and [consumer CI setup](consumer-ci.md) |
| Tool versions and updates | [Pinned versions](../runner/toolchain.json), [Go version](../.go-version), [dependencies](dependencies.md), and [versioning policy](versioning.md) |

## This repository's CI and release policies

These gates protect Levenshtein itself. Adopting the shared checks does not
install this repository's workflows or branch protections in a consumer repo.
[Levenshtein's own CI](maintainers/ci.md) explains the jobs, event mapping,
required checks, and cache trust in detail.

| Policy | Documentation and enforcement |
| --- | --- |
| Verification runs | [Repository config](../levenshtein.json) and [self-check workflow](../.github/workflows/verify.yml): static checks, race tests, consumer and language fixtures, action tests, and release smoke tests |
| Formatting and documentation | [CI hygiene gates](maintainers/ci.md#jobs): gofmt, Ruff, documentation links and indexes, [SDK lock validation](../scripts/test-sdk-lock), and [release pins in examples](../scripts/test-doc-pins) |
| Workflow conventions | [Workflow checks](../scripts/test-workflows): consistent action pins, concurrency groups, merge-queue triggers, and secrets in step environments; [Dependabot](../.github/dependabot.yml) supplies grouped updates |
| Security audits | [Security workflow](../.github/workflows/security.yml): online zizmor audits, OpenSSF Scorecard, and dependency review; [vulnerability workflow](../.github/workflows/vulnerabilities.yml): Go modules and the SDK adapter's dependencies |
| Merge requirements | [Required-check policy](maintainers/ci.md#required-checks-branch-protection), the [committed main ruleset](../.github/rulesets/main.json), and [live drift check](../scripts/test-rulesets) |
| Advisory review and mutation testing | [CI jobs](maintainers/ci.md#jobs) and [mutation policy](mutation.md): separate PR jobs, currently outside the required checks; semantic review skips when its service key is absent |
| Cache isolation and fresh audits | [Result-cache trust](maintainers/ci.md#result-cache-trust) and [cache configuration](maintainers/ci.md#caches-and-self-config-notes) |
| Release builds and publication | [Release procedure](maintainers/releases.md) and [workflow](../.github/workflows/release.yml): release tags must point to `main` and a changelog entry; archives, checksums, SBOMs, and attestations are prepared as a draft for manual publication |
| Release tag protection | [Tag protection instructions](maintainers/releases.md#protecting-release-tags); the [proposed tag rulesets](../.github/rulesets/proposed/tags.json) and [creation ruleset](../.github/rulesets/proposed/tags-creation.json) require an admin to apply them |
| Contributions and local hooks | [Contributing](../CONTRIBUTING.md), [agent conventions](../AGENTS.md), and [optional Lefthook config](../lefthook.yml) |

## Go lint rules

The rules `./verify go-lint` enforces, with the reason for each, are listed in [Go lint rules](rules.md#rules-on-by-default).

## Moved sections

This page used to hold the rules, the evidence for them, and the per-kind details. Each heading below keeps an old link working and says where its content lives now.

### The Staticcheck selection

Moved to [Go lint rules](rules.md#the-staticcheck-selection).

### Changing the selection for one repository

Moved to [Go lint rules](rules.md#changing-the-selection-for-one-repository).

### The modernize selection

The rules that are on moved to [Go lint rules](rules.md#the-modernize-selection), and the ones left off to [rule selection](rule-selection.md#modernize-analyzers-left-off).

### The go-critic selection

The checkers that are on moved to [Go lint rules](rules.md#the-go-critic-selection), and the ones left off to [rule selection](rule-selection.md#go-critic-checkers-left-off).

### Known bug patterns

Moved to [Go lint rules](rules.md#known-bug-patterns).

### Upstream analyzer settings

Moved to [Go lint rules](rules.md#upstream-analyzer-settings).

### Measured on other codebases

Moved to [rule selection](rule-selection.md#measured-on-other-codebases).

### Considered and off

Moved to [rule selection](rule-selection.md#considered-and-off).

### Opt-in complexity: gocognit

Moved to [Go lint rules](rules.md#opt-in-complexity-gocognit).

### Opt-in resources: deferInLoop

Moved to [Go lint rules](rules.md#opt-in-resources-deferinloop).

### Typed choices: LV1001

Moved to [Go lint rules](rules.md#typed-choices-lv1001).

### Construct value records together: LV1002

Moved to [Go lint rules](rules.md#construct-value-records-together-lv1002).

### One field per line: LV1003

Moved to [Go lint rules](rules.md#one-field-per-line-lv1003).

### A blank line between declarations: LV1004

Moved to [Go lint rules](rules.md#a-blank-line-between-declarations-lv1004).

### Formatted files: LV1005

Moved to [Go lint rules](rules.md#formatted-files-lv1005).

### Tests that can fail: LV1006

Moved to [Go lint rules](rules.md#tests-that-can-fail-lv1006).

### Development and exceptions

Moved to [Go lint rules](rules.md): see [suppressing a finding](rules.md#suppressing-a-finding) and [running the linter directly](rules.md#running-the-linter-directly).

### Running the linter directly

Moved to [Go lint rules](rules.md#running-the-linter-directly).

### Named checks and suggested runs

Moved to the [check kinds guide](check-kinds-guide.md#named-checks-and-suggested-runs).

### Module manifests

Moved to the [check kinds guide](check-kinds-guide.md#module-manifests).

### Tests

Moved to the [check kinds guide](check-kinds-guide.md#tests).

### Import boundaries

Moved to the [check kinds guide](check-kinds-guide.md#import-boundaries).

### Generated code

Moved to the [check kinds guide](check-kinds-guide.md#generated-code).

### API compatibility

Moved to the [check kinds guide](check-kinds-guide.md#api-compatibility).

### Workflow security

Moved to the [check kinds guide](check-kinds-guide.md#workflow-security).

### Shell scripts

Moved to the [check kinds guide](check-kinds-guide.md#shell-scripts).

### Secrets

Moved to the [check kinds guide](check-kinds-guide.md#secrets).

### Dependency vulnerabilities

Moved to the [check kinds guide](check-kinds-guide.md#dependency-vulnerabilities).

### Errors and enum switches

Moved to [Go lint rules](rules.md#errors-and-enum-switches).

### Cache and freshness

Moved to the [check kinds guide](check-kinds-guide.md#cache-and-freshness).

### Goroutine leak checks in application tests

Moved to the [check kinds guide](check-kinds-guide.md#goroutine-leak-checks-in-application-tests).
