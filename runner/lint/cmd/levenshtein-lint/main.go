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

	directives "4d63.com/gocheckcompilerdirectives/checkcompilerdirectives"
	errname "github.com/Antonboom/errname/pkg/analyzer"
	testifylint "github.com/Antonboom/testifylint/analyzer"
	"github.com/alingse/nilnesserr"
	"github.com/breml/bidichk/pkg/bidichk"
	perfsprint "github.com/catenacyber/perfsprint/analyzer"
	"github.com/charithe/durationcheck"
	"github.com/ckaznocha/intrange"
	reassign "github.com/curioswitch/go-reassign"
	"github.com/gostaticanalysis/nilerr"
	"github.com/jingyugao/rowserrcheck/passes/rowserr"
	"github.com/kisielk/errcheck/errcheck"
	"github.com/kkHAIKE/contextcheck"
	thelper "github.com/kulti/thelper/pkg/analyzer"
	"github.com/ldez/exptostd"
	"github.com/ldez/usetesting"
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
	"github.com/timonwong/loggercheck"
	"github.com/uudashr/gocognit"
	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"github.com/ykadowak/zerologlint"
	"go-simpler.org/musttag"
	fatcontext "go.augendre.info/fatcontext/pkg/analyzer"
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
		// Staticcheck's runner hands every package fact to its own analyzers
		// regardless of type, and they panic on another analyzer's fact, so
		// contextcheck follows call chains within one package only.
		contextcheck.NewAnalyzer(contextcheck.Configuration{DisableFact: true}),
	}
}

// correctness analyzers report behavior that is wrong, not merely unidiomatic.
func correctness() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		errcheck.Analyzer,
		switches(),
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
		checkedNil(),
		// Nesting through struct pointers stays off: upstream marks it a
		// potential finding, and it cannot tell a context stored for later
		// from one that grows.
		fatcontext.NewAnalyzer(),
	}
}

// switches runs exhaustive over generated files as well. Its own check skips a
// file with a generated header, and cgo gives its rewrite of every
// hand-written file one, which would leave every switch in a package that
// imports "C" unchecked. policy.Adapt still drops findings in files that are
// generated at their source.
func switches() *analysis.Analyzer {
	if err := exhaustive.Analyzer.Flags.Set(exhaustive.CheckGeneratedFlag, "true"); err != nil {
		panic(err)
	}
	return exhaustive.Analyzer
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
// so counting it would report a mix the author cannot fix. cgo's rewrite of a
// hand-written file counts as hand-written.
func receivers() *analysis.Analyzer {
	analyzer := recvcheck.NewAnalyzer(recvcheck.Settings{})
	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		var written []*ast.File
		for _, file := range pass.Files {
			if !policy.Generated(pass.Fset, file) {
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

// checkedNil runs nilnesserr, whose constructor returns an error although it
// has no settings to reject.
func checkedNil() *analysis.Analyzer {
	analyzer, err := nilnesserr.NewAnalyzer(nilnesserr.LinterSetting{})
	if err != nil {
		panic(err)
	}
	return analyzer
}

// logging analyzers report log calls that lose or garble what they were meant
// to record.
func logging() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		zerologlint.Analyzer,
		keyValues(),
	}
}

// nilStringer is loggercheck's report of a pointer argument whose element type
// implements fmt.Stringer.
const nilStringer = "logging value may panic when nil because its element type implements fmt.Stringer"

// keyValues runs loggercheck over logr, klog, zap's sugared logger, and go-kit
// log. go-kit log is off upstream and on here, since an odd key-value list is
// the same bug there. log/slog is left to go vet, whose slog check reports the
// same mistake, so one mistake is one finding. The nil fmt.Stringer report is
// dropped: zap, klog, logr's funcr, and fmt all recover from a String method
// that panics on a nil receiver and print the value as nil.
func keyValues() *analysis.Analyzer {
	analyzer := loggercheck.NewAnalyzer(loggercheck.WithDisable([]string{"slog"}))
	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		report := pass.Report
		filtered := *pass
		filtered.Report = func(diagnostic analysis.Diagnostic) {
			if diagnostic.Message != nilStringer {
				report(diagnostic)
			}
		}
		return run(&filtered)
	}
	return analyzer
}

// source analyzers report text that runs differently than it reads: Unicode
// bidirectional controls that reorder how a line displays, and //go:
// directives that the toolchain ignores because it does not know them.
func source() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		bidichk.NewAnalyzer(),
		directives.Analyzer(),
	}
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
				Syntax:    withoutCgoHeaders(pass),
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

// withoutCgoHeaders hands unparam the package's files with cgo's generated
// header hidden. unparam skips every function in a file whose first comment
// says it is generated, and cgo's rewrite of a hand-written file opens with
// one ahead of the //line directive that maps it to the original. The copies
// keep only the comments of the original, so unparam judges it instead;
// generated files are left alone.
func withoutCgoHeaders(pass *analysis.Pass) []*ast.File {
	files := make([]*ast.File, 0, len(pass.Files))
	for _, file := range pass.Files {
		source := policy.SourceName(pass.Fset, file)
		if source == pass.Fset.File(file.FileStart).Name() || policy.Generated(pass.Fset, file) {
			files = append(files, file)
			continue
		}

		original := *file
		original.Comments = nil
		for _, group := range file.Comments {
			if pass.Fset.Position(group.Pos()).Filename == source {
				original.Comments = append(original.Comments, group)
			}
		}
		files = append(files, &original)
	}
	return files
}

// hygiene analyzers keep the code on modern, consistent standard-library usage.
func hygiene() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		intrange.Analyzer,
		usestdlibvars.New(),
		formatting(),
		predeclared.Analyzer,
		errname.New(),
		exptostd.NewAnalyzer(),
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

// complexity analyzers report functions too hard to follow. They are
// registered so a consumer can select them, and the shipped default in
// runner/toolchain.json turns them off with "-gocognit": a length or nesting
// threshold is a maintenance judgment, not a bug.
func complexity() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		cognitive(),
	}
}

// cognitiveThreshold is the cognitive complexity above which gocognit reports
// a function. Past 30, a function has more branches and nesting than a reader
// can hold at once; it is golangci-lint's default for gocognit.
const cognitiveThreshold = "30"

// cognitive runs gocognit at a fixed threshold. Its -over flag defaults to 0,
// which reports every function.
func cognitive() *analysis.Analyzer {
	if err := gocognit.Analyzer.Flags.Set("over", cognitiveThreshold); err != nil {
		panic(err)
	}
	return gocognit.Analyzer
}

// tests analyzers cover mistakes that only appear in _test.go files.
func tests() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		thelper.NewAnalyzer(),
		tparallel.Analyzer,
		testifylint.New(),
		testingHelpers(),
	}
}

// testingHelpers keeps usetesting on the calls whose testing replacement
// undoes what the test changed. os.Chdir and os.Setenv change process state
// the tests after it see, and os.MkdirTemp and os.CreateTemp in the default
// directory leave files behind; os.Setenv is off upstream and on here.
// os.TempDir, context.Background, and context.TODO stay allowed, since they
// change nothing outside the test.
func testingHelpers() *analysis.Analyzer {
	analyzer := usetesting.NewAnalyzer()
	if err := analyzer.Flags.Set("ossetenv", "true"); err != nil {
		panic(err)
	}
	return analyzer
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
	command.AddBareAnalyzers(policy.Adapt(critics()...)...)
	command.AddBareAnalyzers(policy.Adapt(logging()...)...)
	command.AddBareAnalyzers(policy.Adapt(source()...)...)
	command.AddBareAnalyzers(policy.Adapt(signatures()...)...)
	command.AddBareAnalyzers(policy.Adapt(hygiene()...)...)
	command.AddBareAnalyzers(policy.Adapt(modernizers()...)...)
	command.AddBareAnalyzers(policy.Adapt(tests()...)...)
	command.AddBareAnalyzers(policy.Adapt(complexity()...)...)
	command.AddBareAnalyzers(house()...)

	command.ParseFlags(os.Args[1:])
	command.Run()
}
