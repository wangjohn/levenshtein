# Shared Go lint rules

`./verify go-lint` runs the same pinned checks locally and in any CI provider. Consumer repos inherit the checks by updating their pinned Levenshtein revision. No linter installation is needed outside Dagger.

| Rule | Policy |
| --- | --- |
| `SA*` | All correctness checks in the pinned Staticcheck release |
| `errcheck` | Report implicitly discarded errors; explicit `_ =` remains allowed |
| `exhaustive` | Require enum switches to cover declared values |
| LV1001 | Give enum-like strings defined types and typed constants |
| LV1002 | Construct new structs together with literals, without opt-in markers |

## Typed choices: LV1001

Fields named `Status`, `State`, `Kind`, `Mode`, or `Executor` (case insensitive) must use a defined type instead of plain `string` or an alias of `string`. Other text fields, such as paths and messages, remain ordinary strings. LV1001 also detects enum-like usage regardless of the name, for fields, parameters, and local variables:

- A switch on a string variable or field with at least two distinct nonempty constant choices.
- An OR-chain of equality comparisons, or an AND-chain of inequality comparisons, against at least two distinct nonempty constant choices for the same variable or field.

For example, `priority == "high" || priority == "low"` requires a defined type and typed constants. A single special-case comparison such as `filename == "README.md"` does not. Empty-string checks do not count as enum alternatives. Named string constants and constant expressions count too. These patterns also apply to defined string types with no package-level constants: adding only `type Priority string` does not bypass the requirement for typed constants. References to constants of the matching defined type, including local constants, are accepted.

These are usage heuristics, not proof of a closed domain. A multi-value filename switch can still need a suppression. Calls, indexed expressions, separate comparisons in unrelated statements, and arbitrary validator functions are not inferred as enums. The diagnostic is attached to the switch or comparison so Staticcheck suppression can explain a legitimate open-ended string domain.

```go
type Status string

const (
    StatusPassed Status = "passed"
    StatusFailed Status = "failed"
)

type Result struct {
    Status Status
}

if result.Status == StatusPassed {
    // ...
}
```

For defined string types with package-level typed constants, the check also rejects nonempty string literals and constant expressions used as values, including comparisons, struct literals, assignments, calls, and returns. Declare spellings in constants. Empty zero values and conversions from runtime input remain allowed; this is not runtime enum validation.

## Construct value records together: LV1002

LV1002 applies to all struct types: named, anonymous, local, imported, and aliases. No annotation is required; the former `//levenshtein:record` marker has no special meaning.

Compute intermediate values first, then construct the struct:

```go
return Result{
    Status:     status,
    VerifiedAt: executed.UTC(),
    Error:      message,
}
```

The analyzer tracks new local values made with a literal, `&T{}`, `new(T)`, or a zero-valued `var`. It reports at the creation/declaration when fields are assigned before the value is first used, including nested value fields and construction in `if` branches. One diagnostic covers a construction sequence.

A read, alias, address escape, call using the value, or compound update ends that construction window. Updating parameters, receivers, values returned by factories, or objects already used remains allowed. This conservative local analysis does not follow aliases or prove effects inside callees. Across loops and other complex control flow it stops tracking referenced outer values, while still checking new values created inside their blocks.

The rule promotes clear initialization, not immutability. Whole-value assignments remain allowed. It does not require listing zero-valued fields or force construction to the end of a function. For unavoidable staged setup, put `//lint:ignore LV1002 <reason>` immediately before the reported declaration.

## Development and exceptions

The analyzers use Go's `go/analysis` framework and Staticcheck's runner for package loading, caching, diagnostics, and suppression. LV1001 and LV1002 skip generated Go files; upstream analyzers retain their own generated-code behavior. Analyzer regression fixtures live in `runner/lint/policy/testdata` and run with `cd runner/lint && go test ./...`.

For an exceptional interop requirement, use Staticcheck's normal directive with a reason, for example `//lint:ignore LV1001 external schema requires this field`. Prefer a proper type or record literal when possible.

You can run the same linter directly without Dagger:

```sh
(cd /path/to/levenshtein/runner/lint && go build -o /tmp/levenshtein-lint ./cmd/levenshtein-lint)
cd /path/to/consumer
/tmp/levenshtein-lint -checks=all ./...
```

## Named checks and suggested runs

Runs select checks by name; existing CI still owns triggers and schedules.

| Check | Scope | Suggested use |
| --- | --- | --- |
| `go-lint` | Staticcheck `SA*`, errcheck, exhaustive, LV1001/LV1002 | Branch and pre-merge |
| `go-vet` | The pinned Go toolchain's default vet checks | Branch and pre-merge |
| `go-http` | bodyclose: HTTP response-body closure | HTTP clients/services |
| `go-sql` | sqlclosecheck: deferred SQL rows and statement closure | Database users |
| `workflow-lint` | actionlint: GitHub Actions syntax and expressions | Repos with GitHub Actions |
| `go-vuln` | govulncheck: reachable known vulnerabilities | Dependency updates and daily |
| `self-test` | Levenshtein's own good/bad fixtures | Shared-check development |
| `semantic-lint` | Advisory Jev judgments about Go comments, errors, tests, docs, and PR shape ([details](semantic-lint.md)) | Pull requests, in its own run |

For example, an HTTP service can compose checks using the current versioned interface:

```json
{
  "version": 1,
  "targets": {"app": {"dir": ".", "workspace": ".", "inputs": ["."]}},
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "lint": {"kind": "go-lint", "target": "app", "environment": "go"},
    "http": {"kind": "go-http", "target": "app", "environment": "go"},
    "audit": {"kind": "go-vuln", "target": "app", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["lint", "http"]},
    "dependency-audit": {"checks": ["audit"]},
    "main": {"checks": ["lint", "http", "audit"], "rerun_checks": true}
  }
}
```

Each Go check runs for its selected target. Use a repository-root target (`dir: "."`) for `workflow-lint`; a workflow-less repo should omit it. A repository without `levenshtein.json` gets these checks over a single whole-tree target. HTTP and SQL checks do not replace application tests. ShellCheck and Pyflakes integration is explicitly disabled so results do not depend on optional host tools.

Go vet and standalone tool failures retain native output, including file/line details, inside the report's diagnostic message. Their outer location identifies the module/root rather than pretending the message was parsed into individual source diagnostics. Tool errors never pass; govulncheck's vulnerability exit code is distinguished from network or tool failures.

### Errors and enum switches

Keep errcheck's upstream exclusions for operations documented never to fail. Intentionally ignored errors require explicit `_ =`, preferably with a reason; do not add broad Close/Write exclusions. Check write/flush/close errors when they affect persisted data. A default switch branch does not satisfy exhaustive; list all declared enum values, or use a narrow justified suppression for intentionally partial switches. These checks do not prove runtime enum validity or that an assigned error is handled.

### Cache and freshness

Dagger shares pinned tool builds, dependency downloads, and compiler caches. Staticcheck retains its own analysis cache. Its `SA*` selection expands only when the pinned analyzer version changes; review new findings with dependency upgrades.

Vulnerability data can change without source changes. A `go-vuln` check always bypasses the local result cache, even with `cache: true` and in custom runs. The Dagger executor generates a unique nonce before invoking `sharedCheck`; the nonce enters after tool construction, forcing a new advisory lookup and scan while reusing tool builds. Ordinary checks retain their result caches. Direct Dagger callers must supply a unique `nonce` for each vulnerability invocation.

The report does not claim an immutable vulnerability-database snapshot. Network/database failures fail verification. Levenshtein's daily `main` run includes the scan; consumer CI owns its daily and dependency-change triggers. Pinned standalone tools live in `runner/tools/go.mod`, separate from Staticcheck's analysis dependencies in `runner/lint/go.mod`.

### Goroutine leak checks in application tests

Goroutine leaks require runtime tests, not another static analyzer. A consumer can pin `go.uber.org/goleak v1.3.0` and integrate it at package scope:

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

Use imports `testing` and `go.uber.org/goleak`. Package-level verification works with parallel tests; per-test leak checks can mistake other running tests for leaks. Combine this with the repository's normal tests/race tests, and explicitly account for legitimate background goroutines. Levenshtein does not inject TestMain into consumer packages.
