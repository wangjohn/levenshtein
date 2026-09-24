// Levenshtein-gocheck runs the shared checks that judge a whole Go module
// rather than one package at a time: go-imports, go-generate, and go-apidiff.
// It prints one JSON report on stdout and exits 0 when it has no findings, 1
// when it has some, and 2 when the check could not run, with the reason on
// stderr.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/wangjohn/levenshtein/runner/lint/gocheck"
)

// subcommand names one check.
type subcommand string

const (
	subcommandImports  subcommand = "imports"
	subcommandGenerate subcommand = "generate"
	subcommandApidiff  subcommand = "apidiff"
)

// subcommandNames is how usage errors list the checks.
const subcommandNames = "imports|generate|apidiff"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintf(stderr, "usage: levenshtein-gocheck %s [flags]\n", subcommandNames)
		return gocheck.ExitError
	}

	report, err := check(ctx, subcommand(args[0]), args[1:], os.Environ())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return gocheck.ExitError
	}
	code, err := report.Write(stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
	}
	return code
}

func check(ctx context.Context, name subcommand, args []string, env []string) (gocheck.Report, error) {
	flags := flag.NewFlagSet("levenshtein-gocheck "+string(name), flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	switch name {
	case subcommandImports:
		rules := flags.String("rules", "", "go-imports rules as JSON")
		prefix := flags.String("prefix", ".", "the working directory's path relative to the repository root")
		if err := flags.Parse(args); err != nil {
			return gocheck.Report{}, err
		}
		var config gocheck.ImportRules
		if err := strictJSON(*rules, &config); err != nil {
			return gocheck.Report{}, fmt.Errorf("go-imports rules: %w", err)
		}
		dir, err := os.Getwd()
		if err != nil {
			return gocheck.Report{}, err
		}
		return gocheck.Imports(ctx, dir, *prefix, config, env)
	case subcommandGenerate:
		root := flags.String("root", "", "scratch copy of the repository's inputs, which the check changes")
		module := flags.String("module", ".", "the module's path relative to root")
		if err := flags.Parse(args); err != nil {
			return gocheck.Report{}, err
		}
		if *root == "" {
			return gocheck.Report{}, fmt.Errorf("generate needs -root")
		}
		return gocheck.Generate(ctx, *root, *module, env)
	case subcommandApidiff:
		tool := flags.String("tool", "apidiff", "the pinned apidiff binary")
		base := flags.String("base", "", "the repository's inputs at the merge base")
		head := flags.String("head", "", "the repository's inputs now")
		module := flags.String("module", ".", "the module's path relative to both roots")
		workspace := flags.String("workspace", "off", "the head's go.work relative to its root, or off")
		if err := flags.Parse(args); err != nil {
			return gocheck.Report{}, err
		}
		if *base == "" || *head == "" {
			return gocheck.Report{}, fmt.Errorf("apidiff needs -base and -head")
		}
		return gocheck.Apidiff(ctx, *tool, *base, *head, *module, *workspace, env)
	}
	return gocheck.Report{}, fmt.Errorf("unknown check %q; use %s", name, subcommandNames)
}

// strictJSON decodes exactly one object with no fields the type lacks, so a
// misspelled option is refused rather than ignored.
func strictJSON(data string, value any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}
