// Package community runs community lint rules: go/analysis analyzers that a
// consumer pins in levenshtein.json and that Levenshtein does not ship.
//
// Levenshtein compiles the selected rule modules into a separate binary,
// levenshtein-community-lint, whose generated main calls [Main] with one
// [Module] per rule module. The binary runs beside the core linter on the same
// pinned Staticcheck, and the runner merges both linters' findings into one
// report. See docs/design/community-rules.md in the Levenshtein repository.
//
// Only that generated main imports this package. Rule authors never do: a
// rule module exports plain analyzers from its lvrules package, and that
// contract is what Levenshtein keeps stable. The builder generates the main
// from the same Levenshtein release as this package, so this package's API,
// and the Config and Report JSON it shares with the runner, carry no
// compatibility promise and may change in any release.
//
// This package has no connection to the core linter in runner/lint beyond
// sharing its Staticcheck pin. The failure guard in guard.go and the rule
// adapter in generated.go are generated from the core linter's by go generate
// ./internal/copygen in runner/lint; edit those sources, not the copies.
package community
