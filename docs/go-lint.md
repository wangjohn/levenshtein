# Shared Go lint rules

`./verify go-lint` runs the same pinned checks locally and in any CI provider. Consumer repos inherit the checks by updating their pinned Levenshtein revision. No linter installation is needed outside Dagger.

| Rule | Policy |
| --- | --- |
| `SA*` | All correctness checks in the pinned Staticcheck release |
| `errcheck` | Report implicitly discarded errors; explicit `_ =` remains allowed |
| `exhaustive` | Require enum switches to cover declared values, even with a default branch |
| LV1001 | Give string discriminator fields a defined type and use typed constants for enum values |
| LV1002 | Assemble value records with struct literals instead of assigning their fields throughout a function |

## Typed choices: LV1001

Fields named `Status`, `State`, `Kind`, `Mode`, or `Executor` (case insensitive) must use a defined type instead of plain `string` or an alias of `string`. Other text fields, such as paths and messages, remain ordinary strings. This is a naming convention, not an attempt to infer every enum from its values.

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

Mark package-level structs that represent completed values with `//levenshtein:record` on the type declaration. Compute intermediate values in local variables, then construct the record where it is returned or published:

```go
//levenshtein:record
type Result struct {
    Status     Status
    VerifiedAt time.Time
    Error      string
}

return Result{
    Status:     status,
    VerifiedAt: executed.UTC(),
    Error:      message,
}
```

The rule rejects field writes, increment/decrement operations, and taking addresses of fields on marked records, including aliases, pointer access, and use from another package. Whole-value assignment and struct literals are allowed. State-bearing structs (subprocesses, mutexes, caches) stay unmarked. The marker applies to tests too; build modified test values with a new literal.

This is a source-level construction rule, not deep immutability: it does not prove effects inside arbitrary callees, JSON decoding, or mutations through previously obtained references. It does not require explicitly listing zero-valued fields.

## Development and exceptions

The analyzer dependencies are pinned in `runner/lint/go.mod`; standalone tools are pinned separately in `runner/tools/go.mod` to avoid changing Staticcheck's compatible analysis dependencies.

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

For example, an HTTP service can define:

```json
{
  "modules": ["."],
  "runs": {
    "branch": ["go-lint", "go-vet", "go-http"],
    "pre-merge": ["go-lint", "go-vet", "go-http", "workflow-lint"],
    "main": ["go-lint", "go-vet", "go-http", "workflow-lint", "go-vuln"]
  }
}
```

Every Go check runs for each configured module; workflow lint runs once at the repository root. HTTP and SQL checks do not replace application tests. A workflow-less repo should omit workflow lint. ShellCheck and Pyflakes integration is explicitly disabled so results do not depend on optional host tools.

Go vet and standalone tool failures retain native output, including file/line details, inside the report's diagnostic message. Their outer location identifies the module/root rather than pretending the message was parsed into individual source diagnostics. Tool errors never pass; govulncheck's vulnerability exit code is distinguished from network or tool failures.

### Errors and enum switches

Keep errcheck's upstream exclusions for operations documented never to fail. Intentionally ignored errors require explicit `_ =`, preferably with a reason; do not add broad Close/Write exclusions. Check write/flush/close errors when they affect persisted data. A default switch branch does not satisfy exhaustive; list all declared enum values, or use a narrow justified suppression for intentionally partial switches. These checks do not prove runtime enum validity or that an assigned error is handled.

### Cache and freshness

Dagger shares pinned tool builds, dependency downloads, and compiler caches. Staticcheck retains its own analysis cache. Its `SA*` selection expands only when the pinned analyzer version changes; review new findings with dependency upgrades.

Vulnerability data can change without source changes. The launcher supplies a unique audit nonce on every invocation, including custom run names. This prevents Dagger from reusing the enclosing function result. Only `go-vuln` consumes the audit nonce, **after** tool construction, forcing a new advisory lookup and scan while reusing tool builds and ordinary lint results. Direct Dagger callers must provide a unique `auditNonce`. Do not wrap this check in a source-only result cache. The report does not claim an immutable vulnerability-database snapshot. Network/database failures fail verification. Levenshtein's daily `main` run includes the scan; consumer CI must configure its own daily and dependency-change triggers.

### Goroutine leak checks in application tests

Goroutine leaks require runtime tests, not another static analyzer. A consumer can pin `go.uber.org/goleak v1.3.0` and integrate it at package scope:

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

Use imports `testing` and `go.uber.org/goleak`. Package-level verification works with parallel tests; per-test leak checks can mistake other running tests for leaks. Combine this with the repository's normal tests/race tests, and explicitly account for legitimate background goroutines. Levenshtein does not inject TestMain into consumer packages.
