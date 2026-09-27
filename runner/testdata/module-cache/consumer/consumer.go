// Package consumer depends on example.com/cached, which the module-cache
// self-test serves from a file proxy and then modifies in the module cache.
package consumer

import "example.com/cached"

// Doubled is twice the dependency's value.
func Doubled() int { return 2 * cached.Value() }
