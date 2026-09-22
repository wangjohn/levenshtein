// Package tidy is a module whose go.mod and go.sum are exactly what go mod
// tidy writes, so go-mod passes it.
package tidy

// Answer keeps the module a real package.
func Answer() int { return 42 }
