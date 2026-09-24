// Package store persists the domain model using the standard library alone.
package store

import (
	"os"

	"example.com/imports-good/core"
)

// Save writes a normalized name.
func Save(path, name string) error {
	return os.WriteFile(path, []byte(core.Name(name)), 0o600)
}
