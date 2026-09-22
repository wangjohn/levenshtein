// Package legacy holds the modernize patterns under a Go version that predates
// their replacements, so the version-gated rules must stay silent here.
package legacy

import "strings"

// Minmax clamps below 5 by hand; max arrived in Go 1.21, after this module's go 1.19.
func Minmax(width int) int {
	margin := width / 2
	if margin < 5 {
		margin = 5
	}
	return margin
}

// Mapsloop copies by hand; maps.Copy arrived in Go 1.23.
func Mapsloop(source map[string]string) map[string]string {
	target := map[string]string{}
	for key, value := range source {
		target[key] = value
	}
	return target
}

// Slicescontains searches by hand; slices.Contains arrived in Go 1.21.
func Slicescontains(parts []string) bool {
	for _, part := range parts {
		if part == "testdata" {
			return true
		}
	}
	return false
}

// Stringscutprefix trims by hand; strings.CutPrefix arrived in Go 1.20.
func Stringscutprefix(entry string) string {
	if strings.HasPrefix(entry, "PATH=") {
		return strings.TrimPrefix(entry, "PATH=")
	}
	return ""
}

// Stringsseq ranges over Split; strings.SplitSeq arrived in Go 1.24.
func Stringsseq(path string) int {
	count := 0
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			count++
		}
	}
	return count
}
