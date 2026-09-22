package bad

import "strings"

// minmax: assigning a value and then clamping it is one call to max.
func Minmax(width int) int {
	margin := width / 2
	if margin < 5 {
		margin = 5
	}
	return margin
}

// mapsloop: copying every entry by hand is maps.Copy.
func Mapsloop(source map[string]string) map[string]string {
	target := map[string]string{}
	for key, value := range source {
		target[key] = value
	}
	return target
}

// slicescontains: a loop that only looks for one element is slices.Contains.
func Slicescontains(parts []string) bool {
	for _, part := range parts {
		if part == "testdata" {
			return true
		}
	}
	return false
}

// stringscutprefix: testing a prefix and then trimming it is strings.CutPrefix.
func Stringscutprefix(entry string) string {
	if strings.HasPrefix(entry, "PATH=") {
		return strings.TrimPrefix(entry, "PATH=")
	}
	return ""
}

// stringsseq: ranging over strings.Split allocates a slice the loop never keeps.
func Stringsseq(path string) int {
	count := 0
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			count++
		}
	}
	return count
}
