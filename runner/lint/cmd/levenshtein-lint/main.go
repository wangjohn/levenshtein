// Levenshtein-lint combines shared policy with Staticcheck's cleanup checks.
package main

import (
	"os"

	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"honnef.co/go/tools/lintcmd"
	"honnef.co/go/tools/staticcheck/sa5001"
	"honnef.co/go/tools/staticcheck/sa5003"
	"honnef.co/go/tools/staticcheck/sa9001"
)

func main() {
	command := lintcmd.NewCommand("levenshtein-lint")
	command.AddAnalyzers(sa5001.SCAnalyzer, sa5003.SCAnalyzer, sa9001.SCAnalyzer)
	command.AddBareAnalyzers(policy.TypedValues, policy.Records)
	command.ParseFlags(os.Args[1:])
	command.Run()
}
