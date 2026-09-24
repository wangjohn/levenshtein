package verify

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gocheckVerdict is what an executor must make of one levenshtein-gocheck run.
type gocheckVerdict string

const (
	gocheckPass    gocheckVerdict = "pass"
	gocheckFinding gocheckVerdict = "finding"
	gocheckError   gocheckVerdict = "error"
)

// gocheckCase is one row of runner/testdata/gocheck-reports.json.
type gocheckCase struct {
	Name     string         `json:"name"`
	Exit     int            `json:"exit"`
	Stdout   string         `json:"stdout"`
	Stderr   string         `json:"stderr"`
	Want     gocheckVerdict `json:"want"`
	Findings int            `json:"findings"`
	Notes    int            `json:"notes"`
	Contains string         `json:"contains"`
}

// This reads the same table as the runner's test, so the native executor
// reads levenshtein-gocheck's report exactly as the Dagger path does.
func TestGocheckReportAgreesWithTheRunner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "gocheck-reports.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []gocheckCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("gocheck-reports.json has no cases")
	}

	for _, tc := range table.Cases {
		findings, notes, err := gocheckReport(CheckGoImports, tc.Exit, tc.Stdout, tc.Stderr)
		switch tc.Want {
		case gocheckPass:
			if err != nil || len(findings) != 0 || len(notes) != tc.Notes {
				t.Errorf("%s: want a pass with %d notes: findings=%v notes=%v error=%v", tc.Name, tc.Notes, findings, notes, err)
			}
		case gocheckFinding:
			if err != nil || len(findings) != tc.Findings {
				t.Errorf("%s: want %d findings: findings=%v error=%v", tc.Name, tc.Findings, findings, err)
			}
		case gocheckError:
			if err == nil || !strings.Contains(err.Error(), tc.Contains) {
				t.Errorf("%s: want an error containing %q: findings=%v error=%v", tc.Name, tc.Contains, findings, err)
			}
		default:
			t.Errorf("%s: unknown want %q", tc.Name, tc.Want)
		}
	}
}

// runnerFunctions reads the Dagger functions the runner module exports: its
// exported methods on *Levenshtein, named as Dagger names them, with their
// parameter names.
func runnerFunctions(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join("..", "..", "runner", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string][]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			if ident, ok := star.X.(*ast.Ident); !ok || ident.Name != "Levenshtein" {
				continue
			}
			var params []string
			for _, field := range fn.Type.Params.List {
				for _, param := range field.Names {
					params = append(params, param.Name)
				}
			}
			functions[strings.ToLower(fn.Name.Name[:1])+fn.Name.Name[1:]] = params
		}
	}
	return functions
}

// The Dagger executor calls runner functions by name with named arguments,
// which nothing compiles together, so a renamed function or argument would
// only fail inside an engine. This pins the whole-module checks' calls.
func TestWholeModuleChecksCallRunnerFunctionsThatExist(t *testing.T) {
	functions := runnerFunctions(t)
	for kind, function := range daggerFunctions {
		if _, ok := functions[function]; !ok {
			t.Errorf("%s calls %s, which the runner does not export", kind, function)
		}
	}
	for function, args := range map[string][]string{
		"goImports":  {"source", "rules", "module", "nonce"},
		"goGenerate": {"source", "module", "nonce"},
		"goApidiff":  {"source", "base", "module", "workspace", "nonce"},
	} {
		for _, arg := range args {
			if !slices.Contains(functions[function], arg) {
				t.Errorf("the executor passes %s to %s, whose parameters are %v", arg, function, functions[function])
			}
		}
	}
}

// importRuleTable is runner/testdata/import-rules.json, which the helper's own
// validation also reads.
type importRuleTable struct {
	Fixture ImportsCheck   `json:"fixture"`
	Valid   []ImportsCheck `json:"valid"`
	Invalid []struct {
		Config ImportsCheck `json:"config"`
		Error  string       `json:"error"`
	} `json:"invalid"`
}

func loadImportRuleTable(t *testing.T) importRuleTable {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "import-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table importRuleTable
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Valid) == 0 || len(table.Invalid) == 0 {
		t.Fatal("import-rules.json lost its cases")
	}
	return table
}

// Planning rejects exactly the rules levenshtein-gocheck would reject, so a
// mistake is a configuration error before anything runs.
func TestImportRulesAreValidatedWhenPlanned(t *testing.T) {
	table := loadImportRuleTable(t)
	for _, env := range []Environment{nativeGoEnvironment(), {Executor: ExecutorDagger}} {
		for _, rules := range append(table.Valid, table.Fixture) {
			if err := validateCheck(Check{Kind: CheckGoImports, Imports: &rules}, env); err != nil {
				t.Errorf("%s: valid rules %+v: %v", env.Executor, rules, err)
			}
		}
		for _, tc := range table.Invalid {
			err := validateCheck(Check{Kind: CheckGoImports, Imports: &tc.Config}, env)
			if err == nil || !strings.Contains(err.Error(), tc.Error) {
				t.Errorf("%s: rules %+v: want an error containing %q, got %v", env.Executor, tc.Config, tc.Error, err)
			}
		}
		if err := validateCheck(Check{Kind: CheckGoImports}, env); err == nil || !strings.Contains(err.Error(), `needs an "imports" object`) {
			t.Errorf("%s: go-imports without rules must be refused: %v", env.Executor, err)
		}
	}
}

// Only go-imports takes an imports object, on either executor.
func TestImportsOptionsBelongToGoImports(t *testing.T) {
	rules := &ImportsCheck{Rules: []ImportRule{{Packages: []string{"./..."}, Deny: []string{"unsafe"}, Reason: "no unsafe"}}}
	for _, tc := range []struct {
		kind CheckKind
		env  Environment
	}{
		{CheckGoLint, nativeGoEnvironment()},
		{CheckGoVet, nativeGoEnvironment()},
		{CheckGoLint, Environment{Executor: ExecutorDagger}},
		{CheckGoMutation, Environment{Executor: ExecutorDagger}},
		{CheckSemanticLint, Environment{Executor: ExecutorNative}},
	} {
		err := validateCheck(Check{Kind: tc.kind, Imports: rules}, tc.env)
		if err == nil || !strings.Contains(err.Error(), "only to go-imports") {
			t.Errorf("%s on %s accepted imports options: %v", tc.kind, tc.env.Executor, err)
		}
	}
}

// The option parses from version 1 configuration, a misspelled rule field is
// refused rather than ignored, and the rules key the result, so changing them
// runs the check again.
func TestImportRulesParseAndKeyTheResult(t *testing.T) {
	const config = `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"layers":{"kind":"go-imports","target":"app","environment":"go","imports":{"rules":[%s]}}},"runs":{"branch":{"checks":["layers"]}}}`
	cfg, err := Parse(fmt.Appendf(nil, config, `{"packages":["./core/..."],"deny":["./api/..."],"tests":"exclude","reason":"layering"}`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cfg.Plan(t.TempDir(), "branch")
	if err != nil {
		t.Fatal(err)
	}
	if rules := plan.Checks[0].Check.Imports.Rules; len(rules) != 1 || rules[0].Tests != ImportTestsExclude {
		t.Fatalf("planned rules = %+v", rules)
	}
	if _, err := Parse(fmt.Appendf(nil, config, `{"packages":["./core/..."],"denied":["./api/..."],"reason":"typo"}`)); err == nil {
		t.Fatal(`accepted a misspelled "deny"`)
	}

	for _, executor := range []ExecutorKind{ExecutorNative, ExecutorDagger} {
		req := nativeRequest(t)
		req.Environment.Executor = executor
		key := func(deny string) string {
			t.Helper()
			req.Check = Check{Kind: CheckGoImports, Target: "app", Environment: "host", Imports: &ImportsCheck{Rules: []ImportRule{{Packages: []string{"./..."}, Deny: []string{deny}, Reason: "r"}}}}
			value, err := fingerprint(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		first, again := key("unsafe"), key("unsafe")
		if first == key("reflect") || first != again {
			t.Fatalf("%s: the rules must key the result exactly", executor)
		}
	}
}
