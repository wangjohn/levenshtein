// Levenshtein-lint combines upstream analyzers with shared Go policy.
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"io/fs"
	"maps"
	"os"
	"path/filepath"

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
	"github.com/raeperd/recvcheck"
	sqlclose "github.com/ryanrolds/sqlclosecheck/pkg/analyzer"
	wastedassign "github.com/sanposhiho/wastedassign/v2"
	usestdlibvars "github.com/sashamelentyev/usestdlibvars/pkg/analyzer"
	"github.com/sonatard/noctx"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"go-simpler.org/musttag"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/lintcmd"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"
	"mvdan.cc/unparam/check"
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
		// musttag's built-in list covers encoding/json, encoding/xml, yaml.v3,
		// BurntSushi/toml, mapstructure, and sqlx; a consumer's own wrappers are
		// unknown to a shared linter, so no custom functions are added.
		tagged(),
		// The built-in exclusions keep Unmarshal* and GobDecode, which need a
		// pointer receiver on an otherwise value-receiver type.
		receivers(),
	}
}

// tagged runs musttag with the module of the package being linted. Without
// one, musttag runs `go mod edit -json` in the working directory for every
// package, which names a parent module, or fails, when the linter runs outside
// the package's own module; a wrong module makes musttag skip every named type
// and pass silently.
func tagged() *analysis.Analyzer {
	analyzer := musttag.New()
	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		module, err := modulePath(pass)
		if err != nil {
			return nil, err
		}
		withModule := *pass
		withModule.Module = &analysis.Module{Path: module}
		return run(&withModule)
	}
	return analyzer
}

// modulePath reads the module path from the go.mod nearest a source file of
// the package. Cgo's intermediate files live in the build cache, so the search
// tries each file until one sits inside a module.
func modulePath(pass *analysis.Pass) (string, error) {
	for _, file := range pass.Files {
		name := pass.Fset.Position(file.Package).Filename
		for dir := filepath.Dir(name); ; {
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err == nil {
				if path := modfile.ModulePath(data); path != "" {
					return path, nil
				}
				return "", fmt.Errorf("musttag: %s declares no module path", filepath.Join(dir, "go.mod"))
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("musttag: %w", err)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("musttag: no go.mod above package %s", pass.Pkg.Path())
}

// receivers runs recvcheck over hand-written files only. A generator can
// declare methods on a hand-written type, such as the value-receiver
// MarshalJSON that Dagger's codegen adds to a module's main object, whose
// hand-written methods take pointers. Nobody can change the generated receiver,
// so counting it would report a mix the author cannot fix.
func receivers() *analysis.Analyzer {
	analyzer := recvcheck.NewAnalyzer(recvcheck.Settings{})
	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		var written []*ast.File
		for _, file := range pass.Files {
			if !ast.IsGenerated(file) {
				written = append(written, file)
			}
		}
		results := maps.Clone(pass.ResultOf)
		results[inspect.Analyzer] = inspector.New(written)

		handWritten := *pass
		handWritten.Files = written
		handWritten.ResultOf = results
		return run(&handWritten)
	}
	return analyzer
}

// signatures analyzers report parameters and results that no caller needs.
func signatures() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		unusedParams(),
	}
}

// unusedParams runs unparam over one package at a time. unparam ships a
// checker rather than an analyzer, so this wraps it the way golangci-lint does.
// Exported functions stay out of scope, as in unparam's own default: a
// per-package pass cannot see their callers in other packages, and changing an
// exported signature breaks those callers.
func unusedParams() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     "unparam",
		Doc:      "report unused function parameters and results",
		Requires: []*analysis.Analyzer{buildssa.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			program := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).Pkg.Prog
			checker := &check.Checker{}
			checker.CheckExportedFuncs(false)
			checker.Packages([]*packages.Package{{
				Fset:      pass.Fset,
				Syntax:    pass.Files,
				Types:     pass.Pkg,
				TypesInfo: pass.TypesInfo,
			}})
			checker.ProgramSSA(program)

			issues, err := checker.Check()
			if err != nil {
				return nil, err
			}
			for _, issue := range issues {
				pass.Report(analysis.Diagnostic{
					Pos:     issue.Pos(),
					Message: issue.Message(),
				})
			}
			return nil, nil
		},
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
// repeats intrange). The fix is `go fix -minmax -mapsloop -slicescontains -stringscutprefix -stringsseq ./...`;
// a bare `go fix ./...` also applies the rewrites left off here.
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

// house analyzers are Levenshtein's own rules, documented in docs/checks.md.
func house() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		policy.TypedValues,
		policy.Records,
		policy.Fields,
		policy.Spacing,
		policy.Formatting,
		policy.Assertions,
	}
}

func main() {
	command := lintcmd.NewCommand("levenshtein-lint")
	command.AddAnalyzers(staticcheckFamilies()...)

	command.AddBareAnalyzers(policy.Adapt(resources()...)...)
	command.AddBareAnalyzers(policy.Adapt(correctness()...)...)
	command.AddBareAnalyzers(policy.Adapt(signatures()...)...)
	command.AddBareAnalyzers(policy.Adapt(hygiene()...)...)
	command.AddBareAnalyzers(policy.Adapt(modernizers()...)...)
	command.AddBareAnalyzers(policy.Adapt(tests()...)...)
	command.AddBareAnalyzers(house()...)

	command.ParseFlags(os.Args[1:])
	command.Run()
}
