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

## Rules and check behavior

These summaries explain the rule choices and check behavior. Follow each link for configuration, examples, and limitations.

### The Staticcheck selection

Staticcheck runs its shipped checks except six naming and documentation rules; complexity and loop-resource rules are opt-in. See the [selection and exclusions](rules.md#the-staticcheck-selection).

### Changing the selection for one repository

Use `lint.checks` in `levenshtein.json` to enable or disable rules after the shipped selection. See [repository rule selection](rules.md#changing-the-selection-for-one-repository).

### The modernize selection

Five modernize analyzers replace hand-written constructs with simpler language or standard-library features. See the [enabled rules and fixes](rules.md#the-modernize-selection) and [analyzers left off](rule-selection.md#modernize-analyzers-left-off).

### The go-critic selection

go-critic adds likely-bug checks that complement the other analyzers. See the [enabled checkers](rules.md#the-go-critic-selection) and [checkers left off](rule-selection.md#go-critic-checkers-left-off).

### Known bug patterns

Some analyzers catch established bugs with rare false alarms and local fixes, even without findings in the measured codebases. See [known bug patterns](rules.md#known-bug-patterns).

### Upstream analyzer settings

Upstream analyzers have specific settings and scope limits for contexts, resources, serialization, logging, and tests. See [analyzer settings](rules.md#upstream-analyzer-settings).

### Measured on other codebases

Candidate rules were run on Levenshtein and eight other Go codebases, with findings judged as bugs, taste, or false alarms. See the [measurements and results](rule-selection.md#measured-on-other-codebases).

### Considered and off

Rules stay off when they mostly enforce taste, raise false alarms, duplicate another rule, or lack evidence. See the [excluded analyzers and reasons](rule-selection.md#considered-and-off).

### Opt-in complexity: gocognit

`gocognit` optionally reports functions with cognitive complexity over 30. See [how to enable it](rules.md#opt-in-complexity-gocognit).

### Opt-in resources: deferInLoop

`deferInLoop` optionally reports deferred calls in loops, where resources remain held until the function returns. See [when and how to enable it](rules.md#opt-in-resources-deferinloop).

### Typed choices: LV1001

LV1001 requires defined string types and typed constants for enum-like choices, while keeping free-form text as strings. See [typed choices](rules.md#typed-choices-lv1001).

### Construct value records together: LV1002

LV1002 requires new structs to be initialized together in a literal after computing their fields. See the [construction rules and exceptions](rules.md#construct-value-records-together-lv1002).

### One field per line: LV1003

LV1003 requires each struct field to have its own line. See [field layout](rules.md#one-field-per-line-lv1003).

### A blank line between declarations: LV1004

LV1004 requires a blank line between top-level declarations, above any attached doc comment. See [declaration spacing](rules.md#a-blank-line-between-declarations-lv1004).

### Formatted files: LV1005

LV1005 reports Go files whose formatting differs from `gofmt`. See [formatting scope and fixes](rules.md#formatted-files-lv1005).

### Tests that can fail: LV1006

LV1006 reports tests with no way to fail or an unconditional skip before any checks. See [test requirements and limitations](rules.md#tests-that-can-fail-lv1006).

### Development and exceptions

Explain a local exception with a lint directive; use a baseline for existing findings you intend to fix. See [suppressing a finding](rules.md#suppressing-a-finding) and [running the linter directly](rules.md#running-the-linter-directly).

### Running the linter directly

The Go lint rules can run as a standalone Go command, with Staticcheck flags for rule selection and output. See [commands and version pinning](rules.md#running-the-linter-directly).

### Named checks and suggested runs

Runs select checks by name, with fast checks suited to branch runs and slower or advisory checks to dedicated runs. See [suggested runs and configuration examples](check-kinds-guide.md#named-checks-and-suggested-runs).

### Module manifests

`go-mod` checks that module manifests are tidy and downloaded dependencies match their recorded hashes. See [workspace, vendor, and caching behavior](check-kinds-guide.md#module-manifests).

### Tests

`go-test` runs self-contained unit tests with the race detector and a timeout. See [test behavior and when to use a command check](check-kinds-guide.md#tests).

### Import boundaries

`go-imports` enforces repository-defined rules about which packages may import one another. See [import rules and scope](check-kinds-guide.md#import-boundaries).

### Generated code

`go-generate` runs generators in a scratch copy and fails if their output differs from committed files. See [generation behavior and requirements](check-kinds-guide.md#generated-code).

### API compatibility

`go-apidiff` reports exported API changes that break a library module’s importers, comparing against a base branch’s merge base. See [API comparison and configuration](check-kinds-guide.md#api-compatibility).

### Workflow security

`workflow-security` uses zizmor’s offline audits to report medium- and high-severity GitHub Actions findings. See [audit scope and configuration](check-kinds-guide.md#workflow-security).

### Shell scripts

`shell-lint` uses ShellCheck to report warnings and errors in standalone shell scripts. See [script discovery, exclusions, and configuration](check-kinds-guide.md#shell-scripts).

### Secrets

`secrets` uses gitleaks to scan working-tree files for credentials, with secret values redacted from reports. See [scan scope and allowlisting](check-kinds-guide.md#secrets).

### Dependency vulnerabilities

`go-vuln` checks reachable Go vulnerabilities; `deps-vuln` scans other dependency lockfiles with osv-scanner. See [dependency scan behavior and configuration](check-kinds-guide.md#dependency-vulnerabilities).

### Errors and enum switches

`errcheck` requires explicit handling or discarding of errors, and `exhaustive` requires switches to cover declared enum values. See [error and enum policies](rules.md#errors-and-enum-switches).

### Cache and freshness

Checks reuse cached results where their inputs determine the verdict; vulnerability scans and module verification always run again. See [cache and freshness behavior](check-kinds-guide.md#cache-and-freshness).

### Goroutine leak checks in application tests

Use package-level goleak verification alongside application tests to detect goroutines left running after tests finish. See [integration and parallel-test guidance](check-kinds-guide.md#goroutine-leak-checks-in-application-tests).
