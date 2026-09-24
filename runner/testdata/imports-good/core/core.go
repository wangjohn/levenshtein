// Package core holds the domain model, which knows nothing of transport.
package core

import "strings"

// Name normalizes a user name.
func Name(raw string) string {
	return strings.TrimSpace(raw)
}
