//go:build integration

package bad

// LV1005: the default build leaves this file out, and gofmt still checks it.
func Integration() int {
    return  1
}
