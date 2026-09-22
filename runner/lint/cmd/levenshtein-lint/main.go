// Levenshtein-lint combines upstream analyzers with shared Go policy.
package main

import (
	"os"

	errname "github.com/Antonboom/errname/pkg/analyzer"
	testifylint "github.com/Antonboom/testifylint/analyzer"
	perfsprint "github.com/catenacyber/perfsprint/analyzer"
	"github.com/charithe/durationcheck"
	"github.com/ckaznocha/intrange"
	reassign "github.com/curioswitch/go-reassign"
	"github.com/gostaticanalysis/nilerr"
	"github.com/jingyugao/rowserrcheck/passes/rowserr"
	"github.com/kisielk/errcheck/errcheck"
	thelper "github.com/kulti/thelper/pkg/analyzer"
	"github.com/moricho/tparallel"
	"github.com/nishanths/exhaustive"
	"github.com/nishanths/predeclared/passes/predeclared"
	"github.com/polyfloyd/go-errorlint/errorlint"
	sqlclose "github.com/ryanrolds/sqlclosecheck/pkg/analyzer"
	wastedassign "github.com/sanposhiho/wastedassign/v2"
	usestdlibvars "github.com/sashamelentyev/usestdlibvars/pkg/analyzer"
	"github.com/sonatard/noctx"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/lintcmd"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"
)

// sqlPackages are the database wrappers rowserrcheck follows for an unchecked Rows.Err.
var sqlPackages = []string{
	"database/sql",
	"github.com/jmoiron/sqlx",
}

// staticcheckFamilies is the pinned Staticcheck release in full: SA, S, ST, QF, and U.
func staticcheckFamilies() []*lint.Analyzer {
	var families []*lint.Analyzer
	families = append(families, staticcheck.Analyzers...)
	families = append(families, simple.Analyzers...)
	families = append(families, stylecheck.Analyzers...)
	families = append(families, quickfix.Analyzers...)
	families = append(families, unused.Analyzer)

	for _, family := range families {
		policy.Adapt(family.Analyzer)
	}
	return families
}

// resources analyzers report handles a program opens and never closes.
func resources() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		bodyclose.Analyzer,
		sqlclose.NewDeferOnlyAnalyzer(),
		rowserr.NewAnalyzer(sqlPackages...),
		noctx.Analyzer,
	}
}

// correctness analyzers report behavior that is wrong, not merely unidiomatic.
func correctness() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		errcheck.Analyzer,
		exhaustive.Analyzer,
		nilness.Analyzer,
		unusedwrite.Analyzer,
		errorlint.NewAnalyzer(),
		nilerr.Analyzer,
		durationcheck.Analyzer,
		reassign.NewAnalyzer(),
		wastedassign.Analyzer,
	}
}

// hygiene analyzers keep the code on modern, consistent standard-library usage.
func hygiene() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		intrange.Analyzer,
		usestdlibvars.New(),
		formatting(),
		predeclared.Analyzer,
		errname.New(),
	}
}

// formatting keeps perfsprint on the conversions that actually cost time.
// Its "fiximports" diagnostic only carries an import rewrite for another fix,
// and "error-format" rewrites every constant fmt.Errorf into errors.New,
// which is a spelling preference rather than a cost.
func formatting() *analysis.Analyzer {
	sprint := perfsprint.New()
	for _, name := range []string{"fiximports", "error-format"} {
		if err := sprint.Flags.Set(name, "false"); err != nil {
			panic(err)
		}
	}
	return sprint
}

// modernizers replace hand-written loops and comparisons with the standard
// library call that says the same thing. Go 1.26+ `go fix` applies the whole
// modernize suite; this is the subset measured to fire on real code, with a
// result that reads better, and that no rule above already reports (rangeint
// repeats intrange). The fix is always `go fix ./...`.
func modernizers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		modernize.MinMaxAnalyzer,
		modernize.MapsLoopAnalyzer,
		modernize.SlicesContainsAnalyzer,
		modernize.StringsCutPrefixAnalyzer,
		modernize.StringsSeqAnalyzer,
	}
}

// tests analyzers cover mistakes that only appear in _test.go files.
func tests() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		thelper.NewAnalyzer(),
		tparallel.Analyzer,
		testifylint.New(),
	}
}

// house analyzers encode the conventions in AGENTS.md.
func house() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		policy.TypedValues,
		policy.Records,
		policy.Fields,
		policy.Spacing,
		policy.Formatting,
	}
}

func main() {
	command := lintcmd.NewCommand("levenshtein-lint")
	command.AddAnalyzers(staticcheckFamilies()...)

	command.AddBareAnalyzers(policy.Adapt(resources()...)...)
	command.AddBareAnalyzers(policy.Adapt(correctness()...)...)
	command.AddBareAnalyzers(policy.Adapt(hygiene()...)...)
	command.AddBareAnalyzers(policy.Adapt(modernizers()...)...)
	command.AddBareAnalyzers(policy.Adapt(tests()...)...)
	command.AddBareAnalyzers(house()...)

	command.ParseFlags(os.Args[1:])
	command.Run()
}
