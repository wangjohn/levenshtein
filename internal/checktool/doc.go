// Package checktool is what both executors agree on about each pinned tool a
// shared check runs: the arguments it starts with and how its exit code and
// output become findings or an error.
//
// internal/verify imports it for the native executor. The Dagger runner is a
// separate Go module that cannot import this one, so it compiles a generated
// copy, runner/internal/checktool. Edit this package, never the copy, then run
// go generate ./internal/checktool; a test fails while the copy differs.
package checktool

//go:generate go run ./copygen
