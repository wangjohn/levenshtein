# Shared Go lint rules

`./verify go-lint` runs the same pinned checks locally and in any CI provider. Consumer repos inherit the checks by updating their pinned Levenshtein revision. No linter installation is needed outside Dagger.

| Rule | Policy |
| --- | --- |
| SA5001, SA5003, SA9001 | Staticcheck's defer/close checks |
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

The analyzers use Go's `go/analysis` framework and Staticcheck's runner for package loading, caching, diagnostics, and suppression. Generated Go files are skipped. Analyzer regression fixtures live in `runner/lint/policy/testdata` and run with `cd runner/lint && go test ./...`.

For an exceptional interop requirement, use Staticcheck's normal directive with a reason, for example `//lint:ignore LV1001 external schema requires this field`. Prefer a proper type or record literal when possible.

You can run the same linter directly without Dagger:

```sh
(cd /path/to/levenshtein/runner/lint && go build -o /tmp/levenshtein-lint ./cmd/levenshtein-lint)
cd /path/to/consumer
/tmp/levenshtein-lint -checks=all ./...
```
