// Package community runs community lint rules: go/analysis analyzers that a
// consumer pins in levenshtein.json and that Levenshtein does not ship.
//
// Levenshtein compiles the selected rule modules into a separate binary,
// levenshtein-community-lint, whose generated main calls [Main] with one
// [Module] per rule module. The binary runs beside the core linter on the same
// pinned Staticcheck, and the runner merges both linters' findings into one
// report. See docs/community-rules.md in the Levenshtein repository.
//
// This package has no connection to the core linter in runner/lint beyond
// sharing its Staticcheck pin. The failure guard in guard.go is a copy of the
// core linter's; change both together.
package community
