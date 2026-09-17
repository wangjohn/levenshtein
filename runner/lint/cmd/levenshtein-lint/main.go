// Levenshtein-lint combines upstream analyzers with shared Go policy.
package main

import (
	"os"

	"github.com/kisielk/errcheck/errcheck"
	"github.com/nishanths/exhaustive"
	sqlclose "github.com/ryanrolds/sqlclosecheck/pkg/analyzer"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"honnef.co/go/tools/lintcmd"
	"honnef.co/go/tools/staticcheck"
)

func main() {
	command := lintcmd.NewCommand("levenshtein-lint")
	command.AddAnalyzers(staticcheck.Analyzers...)
	command.AddBareAnalyzers(errcheck.Analyzer, exhaustive.Analyzer, bodyclose.Analyzer, sqlclose.NewDeferOnlyAnalyzer())
	command.AddBareAnalyzers(policy.TypedValues, policy.Records)
	command.ParseFlags(os.Args[1:])
	command.Run()
}
