package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestKeyValues keeps loggercheck reporting odd key-value lists while leaving
// log/slog to go vet and dropping the nil fmt.Stringer report. A loggercheck
// upgrade that rewords that report makes it show up here unexpectedly.
func TestKeyValues(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), keyValues(), "keyvalues")
}
