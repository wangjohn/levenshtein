# Shared Go lint rules

`./verify go-lint` runs the same pinned checks locally and in any CI provider. Consumer repos inherit the checks by updating their pinned Levenshtein revision. No linter installation is needed outside Dagger.

| Rule | Policy |
| --- | --- |
| SA5001, SA5003, SA9001 | Staticcheck's defer/close checks |
| LV1001 | Give enum-like strings defined types and typed constants |
| LV1002 | Construct new structs together with literals, without opt-in markers |

## Typed choices: LV1001

Fields named `Status`, `State`, `Kind`, `Mode`, or `Executor` (case insensitive) must use a defined type instead of plain `string` or an alias of `string`. Other text fields, such as paths and messages, remain ordinary strings. LV1001 also detects enum-like usage regardless of the name, for fields, parameters, and local variables:

- A switch on a plain string variable or field with at least two distinct nonempty constant choices.
- An OR-chain of equality comparisons, or an AND-chain of inequality comparisons, against at least two distinct nonempty constant choices for the same variable or field.

For example, `priority == "high" || priority == "low"` requires a defined type and typed constants. A single special-case comparison such as `filename == "README.md"` does not. Empty-string checks do not count as enum alternatives. Named string constants and constant expressions count too.

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

The analyzers use Go's `go/analysis` framework and Staticcheck's runner for package loading, caching, diagnostics, and suppression. Generated Go files are skipped. Analyzer regression fixtures live in `runner/lint/policy/testdata` and run with `cd runner/lint && go test ./...`.

For an exceptional interop requirement, use Staticcheck's normal directive with a reason, for example `//lint:ignore LV1001 external schema requires this field`. Prefer a proper type or record literal when possible.

You can run the same linter directly without Dagger:

```sh
(cd /path/to/levenshtein/runner/lint && go build -o /tmp/levenshtein-lint ./cmd/levenshtein-lint)
cd /path/to/consumer
/tmp/levenshtein-lint -checks=all ./...
```
