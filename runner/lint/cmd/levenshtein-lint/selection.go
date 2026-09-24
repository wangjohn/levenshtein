package main

import (
	"flag"
	"strings"
	"unicode"
)

// checkList is the parsed -checks flag, or nil when it is Staticcheck's
// default "inherit", which leaves the choice to staticcheck.conf.
func checkList(flags *flag.FlagSet) []string {
	value := strings.Trim(flags.Lookup("checks").Value.String(), `"`)
	if value == "" || value == "inherit" {
		return nil
	}
	checks := strings.Split(value, ",")
	for i, check := range checks {
		checks[i] = strings.TrimSpace(check)
	}
	return checks
}

// allowed reproduces Staticcheck's filterAnalyzerNames (lintcmd/lint.go in
// honnef.co/go/tools v0.8.1) for one code. runner/main.go and
// internal/verify/findings.go keep copies; all three load
// runner/testdata/selection.json in their tests. Patterns apply in order and
// the last one that matches wins; a "-" prefix turns a code off.
func allowed(checks []string, code string) bool {
	selected := false
	for _, check := range checks {
		pattern := check
		enable := true
		if len(pattern) > 1 && pattern[0] == '-' {
			pattern = pattern[1:]
			enable = false
		}
		if selects(pattern, code) {
			selected = enable
		}
	}
	return selected
}

// selects matches one pattern the way Staticcheck does, ignoring case: "all"
// or "*" matches every code, a trailing "*" after letters matches that exact
// category (S* matches S1002 but not SA5001), a trailing "*" after a digit is a
// plain prefix (SA5* matches SA5001), and anything else is a literal name.
func selects(pattern, code string) bool {
	pattern = strings.ToLower(pattern)
	code = strings.ToLower(code)

	//lint:ignore LV1001 patterns are free-form user input; these are two spellings of one wildcard, not an enum.
	if pattern == "*" || pattern == "all" {
		return true
	}
	prefix, glob := strings.CutSuffix(pattern, "*")
	if !glob {
		return pattern == code
	}
	if strings.IndexFunc(prefix, unicode.IsNumber) != -1 {
		return strings.HasPrefix(code, prefix)
	}
	category := code
	if digit := strings.IndexFunc(code, unicode.IsNumber); digit != -1 {
		category = code[:digit]
	}
	return category == prefix
}
