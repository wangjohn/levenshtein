# Example rule module

A community lint rule module in the shape described in
[community lint rules](../../docs/community-rules.md). It is its own Go module,
so no Levenshtein build depends on it; Levenshtein's CI builds it into a
community linter to keep it working (`scripts/test-example-rules`).

Namespace: `example`. Every rule reports as `example_<rule>`.

## Rules

### nopanic

`example_nopanic` reports a call to the builtin `panic` in library code, where a
caller cannot handle the failure.

- **Allowed:** package `main`, `_test.go` files, `init` functions, and functions
  named `Must` or `Must<Upper>...` (so `MustParse`, but not `Mustard`). A local
  function that happens to be named `panic` is not reported.
- **Fix:** return an error, or add a `Must` variant for callers that want the
  panic.
- **Suppress one site:** `//lint:ignore example_nopanic <reason>` on the line
  above.

## Use it

> [!NOTE]
> Levenshtein does not load rule modules yet; this is the proposed configuration.

```json
"rules": {
  "github.com/wangjohn/levenshtein/examples/rule-module": {"version": "v0.1.0", "select": ["example_*"]}
}
```

Until then, run the rule on its own with the `singlechecker` wrapper, which
also applies fixes with `-fix` for rules that suggest them:

```sh
go run github.com/wangjohn/levenshtein/examples/rule-module/cmd/nopanic@latest ./...
```

## Layout

- `lvrules/`: `Namespace` and `Analyzers()`, the only package Levenshtein imports.
- `nopanic/`: the rule and its `analysistest` fixtures.
- `cmd/nopanic/`: a `singlechecker` wrapper for running the rule on its own.

## Develop

```sh
cd examples/rule-module
GOWORK=off go test ./...
GOWORK=off go run ./cmd/nopanic ./nopanic/testdata/src/library
```
