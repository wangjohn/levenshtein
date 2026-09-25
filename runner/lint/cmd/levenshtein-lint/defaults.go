package main

import (
	"flag"
	"strings"
)

// shippedChecks is the default rule selection, a copy of "checks" in
// runner/toolchain.json, which this module cannot read once it is installed
// with go run or go install. TestShippedChecksMatchToolchain keeps the two
// equal. The runner always passes -checks, so this default only decides what
// a direct run reports.
var shippedChecks = []string{
	"all",
	"-ST1000",
	"-ST1003",
	"-ST1016",
	"-ST1020",
	"-ST1021",
	"-ST1022",
	"-gocognit",
	"-deferInLoop",
}

// selectShipped makes a lint run without -checks report the shipped selection
// rather than Staticcheck's default, which would also turn on the opt-in
// rules. An explicit -checks, including -checks=inherit to defer to
// staticcheck.conf, is left as given.
func selectShipped(flags *flag.FlagSet) error {
	given := false
	flags.Visit(func(set *flag.Flag) {
		if set.Name == "checks" {
			given = true
		}
	})
	if given || !linting(flags) {
		return nil
	}
	return flags.Set("checks", strings.Join(shippedChecks, ","))
}
