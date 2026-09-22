// Package untidy is a module go mod tidy would rewrite: go.mod requires a
// module nothing imports and go.sum lists one nothing requires. go-mod fails
// it with the diff, without reaching a module proxy.
package untidy

// Answer keeps the module a real package.
func Answer() int { return 42 }
