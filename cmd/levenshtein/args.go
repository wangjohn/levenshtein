package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/pflag"
	"github.com/wangjohn/levenshtein/internal/verify"
)

type options struct {
	cacheDir   string
	source     string
	shared     string
	name       string
	jobs       int
	dry        bool
	format     verify.Format
	pathPrefix string
	render     string
}

func formatNames() string {
	names := make([]string, 0, len(verify.Formats))
	for _, format := range verify.Formats {
		names = append(names, string(format))
	}
	return strings.Join(names, ", ")
}

func parseArgs(args []string, opts options, output io.Writer) (options, error) {
	flags := pflag.NewFlagSet("verify", pflag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.source, "source", opts.source, "Repository directory to verify")
	flags.StringVar(&opts.shared, "shared", opts.shared, "Pinned Levenshtein checkout")
	flags.StringVar(&opts.cacheDir, "cache-dir", opts.cacheDir, "Verification cache directory outside source and shared checkouts")
	flags.IntVar(&opts.jobs, "jobs", opts.jobs, "Maximum checks to run at once (0 keeps the default cap)")
	flags.BoolVar(&opts.dry, "dry-run", false, "Print the verification plan without running checks")
	format := flags.String("format", string(verify.FormatJSON), "Report format: "+formatNames())
	flags.StringVar(&opts.pathPrefix, "path-prefix", "", "Directory joined in front of report paths in text, github and sarif output")
	flags.StringVar(&opts.render, "render", "", "Write a saved JSON report (a file, or - for stdin) in --format instead of running checks")
	help := flags.BoolP("help", "h", false, "Show usage")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(output, "Usage: verify [RUN] [flags]\n       verify --render REPORT --format FORMAT\n\nRUN defaults to branch. Flags may appear before or after RUN.\n\nFlags:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if *help {
		flags.Usage()
		return opts, pflag.ErrHelp
	}
	if flags.NArg() > 1 {
		return opts, fmt.Errorf("expected at most one run, got %q", flags.Args())
	}

	opts.format = verify.Format(*format)
	if !slices.Contains(verify.Formats, opts.format) {
		return opts, fmt.Errorf("--format must be one of %s", formatNames())
	}
	if err := checkModes(opts, flags.NArg()); err != nil {
		return opts, err
	}
	if opts.pathPrefix != "" {
		opts.pathPrefix = filepath.ToSlash(filepath.Clean(opts.pathPrefix))
	}

	opts.name = "branch"
	if flags.NArg() == 1 {
		opts.name = flags.Arg(0)
	}
	if opts.source == "" {
		return opts, fmt.Errorf("source directory cannot be empty")
	}
	if opts.jobs < 0 {
		return opts, fmt.Errorf("--jobs cannot be negative")
	}
	return opts, nil
}

// checkModes rejects flag combinations that would silently mean nothing.
func checkModes(opts options, runs int) error {
	switch {
	case opts.render != "" && (runs != 0 || opts.dry):
		return fmt.Errorf("--render writes a saved report and runs nothing; it takes no run or --dry-run")
	case opts.dry && opts.format != verify.FormatJSON:
		return fmt.Errorf("--dry-run prints the plan as JSON and runs nothing; it takes no --format")
	case opts.pathPrefix != "" && opts.format == verify.FormatJSON:
		return fmt.Errorf("--path-prefix applies to text, github and sarif output, not json")
	case opts.pathPrefix != "" && !filepath.IsLocal(filepath.Clean(opts.pathPrefix)) && filepath.Clean(opts.pathPrefix) != ".":
		return fmt.Errorf("--path-prefix %q must be a relative directory inside the checkout", opts.pathPrefix)
	}
	return nil
}
