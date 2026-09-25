// Package ignored carries a //lint:ignore directive for nilerr, which upstream
// leaves for Staticcheck to apply: the adapted analyzer still reports the
// finding so the directive has something to match.
package ignored

import "os"

// Size returns 0 for a missing file rather than failing.
func Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		//lint:ignore nilerr a missing file has size zero by design
		return 0, nil // want "error is not nil"
	}
	return info.Size(), nil
}
