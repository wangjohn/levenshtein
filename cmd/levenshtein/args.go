package main

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"
)

type options struct {
	cacheDir string
	source   string
	shared   string
	name     string
	dry      bool
}

func parseArgs(args []string, opts options, output io.Writer) (options, error) {
	flags := pflag.NewFlagSet("verify", pflag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.source, "source", opts.source, "Repository directory to verify")
	flags.StringVar(&opts.shared, "shared", opts.shared, "Pinned Levenshtein checkout")
	flags.StringVar(&opts.cacheDir, "cache-dir", opts.cacheDir, "Verification cache directory outside source and shared checkouts")
	flags.BoolVar(&opts.dry, "dry-run", false, "Print the verification plan without running checks")
	help := flags.BoolP("help", "h", false, "Show usage")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(output, "Usage: verify [RUN] [flags]\n\nRUN defaults to branch. Flags may appear before or after RUN.\n\nFlags:")
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

	opts.name = "branch"
	if flags.NArg() == 1 {
		opts.name = flags.Arg(0)
	}
	if opts.source == "" {
		return opts, fmt.Errorf("source directory cannot be empty")
	}
	return opts, nil
}
