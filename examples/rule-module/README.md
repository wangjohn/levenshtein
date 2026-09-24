# Example rule module

A community lint rule module in the shape proposed in
[community lint rules](../../docs/community-rules.md). It is its own Go module,
so no Levenshtein build, check, or `go.mod` depends on it.

## nopanic

Reports a call to the builtin `panic` in library code, where a caller cannot
handle the failure. Return an error instead.

Package `main`, `init` functions, and functions whose names start with `Must`
may panic. A local function that happens to be named `panic` is not reported.

## Layout

- `levenshtein/`: `Analyzers()`, the only package Levenshtein would import.
- `nopanic/`: the rule and its `analysistest` fixtures.
- `cmd/nopanic/`: a `singlechecker` wrapper for running the rule on its own.

## Try it

```sh
cd examples/rule-module
GOWORK=off go test ./...
GOWORK=off go run ./cmd/nopanic ./nopanic/testdata/src/library
```
