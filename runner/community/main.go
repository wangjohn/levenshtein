package community

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/lintcmd"
)

// Name is the community linter's command name.
const Name = "levenshtein-community-lint"

// Main runs the community linter over the modules the generated main passes
// and exits. Besides Staticcheck's own flags, it takes -lvrules.config, the
// check's Config as JSON, and -lvrules.report, where it writes its Report
// once Staticcheck has finished. It sets -checks and -fail itself.
func Main(modules []Module) {
	os.Exit(run(modules, os.Args[1:]))
}

// Exit codes beyond Staticcheck's own 0 (no failing findings) and 1 (failing
// findings, or a run that could not finish).
const (
	exitConfiguration = 2
	exitReport        = 3
)

func run(modules []Module, args []string) int {
	command := lintcmd.NewCommand(Name)
	flags := command.FlagSet()
	configPath := flags.String("lvrules.config", "", "read the check's rule module `configuration` (JSON) from this file")
	reportPath := flags.String("lvrules.report", "", "write the run's report (JSON) to this `file` once it finishes")
	command.ParseFlags(args)

	resolved, err := load(modules, flags, *configPath, *reportPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return exitConfiguration
	}
	if len(resolved.Selected) == 0 {
		return writeReport(*reportPath, resolved.Report, 0)
	}

	retire, err := cacheGeneration()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return exitConfiguration
	}
	guard := newGuard(retire)
	analyzers := make([]*analysis.Analyzer, 0, len(resolved.Selected)+2)
	for _, rule := range resolved.Selected {
		rule.Analyzer.Name = rule.Code
		rule.Analyzer.Doc = titled(rule.Analyzer.Doc)
		guard.wrap(rule.Analyzer, owner{Code: rule.Code, Source: rule.Module.Source()})
		analyzers = append(analyzers, rule.Analyzer)
	}
	analyzers = append(analyzers, mixedAnalyzer(), renamedAnalyzer(resolved.Renames))
	command.AddBareAnalyzers(analyzers...)

	codes := make([]string, 0, len(analyzers))
	failing := make([]string, 0, len(analyzers))
	for _, analyzer := range analyzers {
		codes = append(codes, analyzer.Name)
		if !resolved.Advisory[strings.ToLower(analyzer.Name)] {
			failing = append(failing, analyzer.Name)
		}
	}
	if err := flags.Set("checks", strings.Join(codes, ",")); err != nil {
		panic(err) // A list flag accepts any string.
	}
	if err := flags.Set("fail", strings.Join(failing, ",")); err != nil {
		panic(err)
	}

	exit, err := execute(command, flags, resolved.advisory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return exitReport
	}
	report := resolved.Report
	report.Failures = guard.report()
	return writeReport(*reportPath, report, exit)
}

// ownedFlags are the Staticcheck flags the linter sets from its configuration.
var ownedFlags = []string{"checks", "fail"}

// load reads the check's configuration, resolves it against the modules, and
// applies its settings.
func load(modules []Module, flags *flag.FlagSet, configPath, reportPath string) (plan, error) {
	if configPath == "" || reportPath == "" {
		return plan{}, errors.New("-lvrules.config and -lvrules.report are required")
	}
	var passed []string
	flags.Visit(func(f *flag.Flag) { passed = append(passed, f.Name) })
	for _, name := range passed {
		if slices.Contains(ownedFlags, name) {
			return plan{}, fmt.Errorf("-%s is set from -lvrules.config; select rules there instead", name)
		}
	}

	data, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return plan{}, err
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return plan{}, fmt.Errorf("reading %s: %w", configPath, err)
	}

	resolved, err := resolve(modules, cfg)
	if err != nil {
		return plan{}, err
	}
	for _, setting := range resolved.Settings {
		if err := setting.Rule.Analyzer.Flags.Set(setting.Flag, setting.Value); err != nil {
			return plan{}, fmt.Errorf("%s: setting %s %s=%q: %w", setting.Rule.Module.Source(), setting.Rule.Code, setting.Flag, setting.Value, err)
		}
	}
	return resolved, nil
}

// execute runs Staticcheck. JSON output is captured and passed through
// advise; any other format goes straight to stdout.
func execute(command *lintcmd.Command, flags *flag.FlagSet, advisory func(string) bool) (int, error) {
	if flags.Lookup("f").Value.String() != "json" {
		return command.Execute(), nil
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	stdout := os.Stdout
	os.Stdout = writer
	captured := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(reader) // A read error surfaces as truncated JSON below.
		captured <- data
	}()

	exit := command.Execute()
	os.Stdout = stdout
	if err := writer.Close(); err != nil {
		return 0, err
	}
	output := <-captured
	if err := reader.Close(); err != nil {
		return 0, err
	}

	rewritten, exit, err := advise(output, exit, advisory)
	if err != nil {
		return 0, err
	}
	if _, err := stdout.Write(rewritten); err != nil {
		return 0, err
	}
	return exit, nil
}

// advisory reports whether a code an ignore directive names, which may be a
// glob, covers only advisory rules. A code that covers no selected rule is
// not advisory.
func (p plan) advisory(code string) bool {
	covered := false
	for _, rule := range p.Selected {
		lowered := strings.ToLower(rule.Code)
		if match, _ := filepath.Match(strings.ToLower(code), lowered); !match {
			continue
		}
		if !p.Advisory[lowered] {
			return false
		}
		covered = true
	}
	return covered
}

// writeReport writes the report and returns exit, or exitReport when the
// report cannot be written: without one, the runner cannot trust the run.
func writeReport(path string, report Report, exit int) int {
	data, err := json.Marshal(report)
	if err == nil {
		err = os.WriteFile(path, append(data, '\n'), 0o600)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: writing the report: %v\n", Name, err)
		return exitReport
	}
	return exit
}
