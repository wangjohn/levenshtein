package community

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
// once Staticcheck has finished, or at the first rule failure, when it stops.
// It sets -checks and -fail itself.
func Main(modules []Module) {
	os.Exit(run(modules, os.Args[1:]))
}

// Exit codes beyond Staticcheck's own 0 (no failing findings) and 1 (failing
// findings, or a run that could not finish).
const (
	exitConfiguration = 2
	exitReport        = 3
	exitRuleFailed    = 4
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

	if err := settingsCache(resolved.Settings); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return exitConfiguration
	}

	analyzers := resolved.register(func(failed failure) {
		report := resolved.Report
		report.Failures = []Failure{{Code: failed.Code, Source: failed.Source, Package: failed.Package, Error: failed.Error}}
		os.Exit(writeReport(*reportPath, report, exitRuleFailed))
	})
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
	return writeReport(*reportPath, resolved.Report, exit)
}

// register renames each selected rule to its code, guards it and everything it
// requires, and returns the analyzers to register, the linter's own last.
// Every rule takes its code before any is wrapped: a rule can require
// another, and the guard names a failure by the name it finds first.
func (p plan) register(stop func(failure)) []*analysis.Analyzer {
	for _, rule := range p.Selected {
		rule.Analyzer.Name = rule.Code
		rule.Analyzer.Doc = titled(rule.Analyzer.Doc)
	}

	guard := newGuard(stop)
	analyzers := make([]*analysis.Analyzer, 0, len(p.Selected)+2)
	for _, rule := range p.Selected {
		guard.wrap(rule.Analyzer, owner{Code: rule.Code, Source: rule.Module.Source()})
		analyzers = append(analyzers, rule.Analyzer)
	}
	return append(analyzers, mixedAnalyzer(), renamedAnalyzer(p.Renames))
}

// settingsCache gives each distinct set of rule settings its own Staticcheck
// cache. Settings reach a rule through its analyzer's flags, which
// Staticcheck's cache key leaves out, so two runs with different settings
// would otherwise share results. It must run before Staticcheck first reads
// STATICCHECK_CACHE. An explicit "off", or a relative path Staticcheck will
// refuse, is left alone.
func settingsCache(settings []setting) error {
	if len(settings) == 0 {
		return nil
	}
	base := os.Getenv("STATICCHECK_CACHE")
	if base == "off" || (base != "" && !filepath.IsAbs(base)) {
		return nil
	}
	if base == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("STATICCHECK_CACHE is not set and there is no user cache directory: %w", err)
		}
		base = filepath.Join(dir, "staticcheck")
	}
	return os.Setenv("STATICCHECK_CACHE", filepath.Join(base, "settings-"+settingsDigest(settings)))
}

// settingsDigest identifies a set of settings, whatever order they came in.
func settingsDigest(settings []setting) string {
	lines := make([]string, 0, len(settings))
	for _, s := range settings {
		lines = append(lines, strings.Join([]string{s.Rule.Code, s.Flag, s.Value}, "\x00"))
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
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
