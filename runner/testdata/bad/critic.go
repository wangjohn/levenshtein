package bad

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Split holds two slices that append results can mix up.
type Split struct {
	positives []int
	negatives []int
}

// appendAssign: the result of appending to negatives lands in positives.
func (s *Split) Add(value int) {
	s.positives = append(s.negatives, value)
}

// argOrder: the prefix and the string are swapped.
func ArgOrder(line string) bool {
	return strings.HasPrefix("#", line)
}

// badCall: a count of zero makes SplitN return nothing.
func BadCall(line string) []string {
	return strings.SplitN(line, ",", 0)
}

// badCond: no value is both below 5 and above 10.
func BadCond(value int) bool {
	return value < 5 && value > 10
}

// badRegexp: a character class lists the same character twice.
var badRegexp = regexp.MustCompile(`[aa]`)

// BadRegexp uses the pattern so it is not dead code.
func BadRegexp(text string) bool {
	return badRegexp.MatchString(text)
}

// FuncOld is kept for old callers.
//
// deprecatedComment: tools only recognize the exact "Deprecated:" form.
//
// Deprecated, use Double instead.
func FuncOld(value int) int {
	return value * 2
}

// dupArg: copying a slice onto itself does nothing.
func DupArg(values []int) int {
	return copy(values, values)
}

// dupBranchBody: both branches do the same thing.
func DupBranchBody(ok bool) string {
	if ok {
		return "done"
	} else {
		return "done"
	}
}

// dupCase: the second case can never match.
func DupCase(value int, choices []int) bool {
	switch value {
	case choices[0], choices[0]:
		return true
	}
	return false
}

// exitAfterDefer: log.Fatal exits without running the deferred cleanup.
func ExitAfterDefer(path string) {
	defer func() { _ = os.Remove(path) }()
	if path == "" {
		log.Fatal("no path")
	}
}

// filepathJoin: a separator inside one element defeats the join on Windows.
func FilepathJoin(dir string) string {
	return filepath.Join(dir, "reports/test.txt")
}

// flagDeref: dereferencing at definition reads the default, not the flag.
func FlagDeref() bool {
	return *flag.Bool("verbose", false, "log more")
}

// flagName: a flag name with spaces cannot be passed on a command line.
func FlagName() *bool {
	return flag.Bool(" quiet", false, "log less")
}

// mapKey: the trailing space makes a key no caller will type.
func MapKey() map[string]int {
	return map[string]int{
		"one":  1,
		"two ": 2,
	}
}

// offBy1: indexing at the length always panics.
func OffBy1(values []int) int {
	return values[len(values)]
}
