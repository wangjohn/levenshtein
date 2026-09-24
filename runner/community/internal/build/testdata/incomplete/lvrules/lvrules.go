// Package lvrules imports a package its go.mod does not require, so its
// resolution would depend on whatever the module proxy serves that day.
package lvrules

import (
	"rsc.io/quote"
	"golang.org/x/tools/go/analysis"
)

const Namespace = "incomplete"

var _ = quote.Hello

func Analyzers() []*analysis.Analyzer { return nil }
