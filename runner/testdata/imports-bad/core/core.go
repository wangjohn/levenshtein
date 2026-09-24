// Package core is supposed to know nothing of transport.
package core

import (
	"strings"

	"example.com/imports-bad/api"
)

// Name normalizes a user name.
func Name(raw string) string {
	return strings.TrimSpace(raw) + api.Suffix
}
