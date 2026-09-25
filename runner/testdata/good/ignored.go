package good

import "os"

// Size returns 0 for a missing file rather than failing. nilerr reports the
// discarded error, and the //lint:ignore directive must suppress exactly that
// finding: nilerr must neither drop it first nor leave the directive unused.
func Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		//lint:ignore nilerr a missing file has size zero by design
		return 0, nil
	}
	return info.Size(), nil
}
