# Shared checks

Levenshtein verifies a repository with checks, and one of those checks, `go-lint`, is made of rules.

- A **check** is one entry in `levenshtein.json`: a [kind](check-kinds.md), such as `go-lint`, `go-vet`, or `secrets`, run over a target in an environment. Most kinds wrap one pinned tool, such as `go vet`, ShellCheck, or gitleaks, and report what it reports.
- A **rule** is one analyzer inside `go-lint`, reported under its own code, such as `SA4006`, `errcheck`, or `LV1001`. A lint finding's `url` leads to its rule's documentation.

Every check kind runs the same pinned tools locally and in any CI provider, and consumer repositories pick up a change by updating their pinned Levenshtein revision. How a check gets its tools depends on its environment's executor. On `dagger`, the check runs in a container that carries the pinned Go toolchain and tools, so the host needs only a Docker-compatible runtime. On `native`, the check runs on the host: it needs the Go version in `.go-version` on `PATH`, and Levenshtein builds or downloads the other pinned tools itself, checking each download against its pinned SHA-256. [Setup](setup.md#prerequisites) and [native Go checks](configuration.md#native-go-checks) have the details.

Where to read next:

- [Check kinds](check-kinds.md): the generated table of every kind, its executors, caching, baseline support, and default runs.
- [Check kinds guide](check-kinds-guide.md): which run each kind belongs in, and how the kinds that wrap a whole tool behave.
- [Go lint rules](rules.md): every rule `go-lint` enforces, why, and how to suppress a finding.
- [Rule selection](rule-selection.md): how rules are chosen, what was measured, and every analyzer considered and left off.

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
