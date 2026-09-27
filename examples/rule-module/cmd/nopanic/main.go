// Nopanic runs the rule on its own, outside Levenshtein's community linter:
// go run ./cmd/nopanic ./... or go vet -vettool.
package main

import (
	"github.com/wangjohn/levenshtein/examples/rule-module/nopanic"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(nopanic.Analyzer)
}
