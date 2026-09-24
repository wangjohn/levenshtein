// Package files holds one defer inside a loop. The shipped default selection
// leaves deferInLoop off, so the default passes here and -checks=deferInLoop
// fails.
package files

import (
	"io"
	"os"
)

// Size adds up the sizes of the named files. deferInLoop: every file stays open
// until Size returns, not until its own pass of the loop ends.
func Size(names []string) (int64, error) {
	var total int64
	for _, name := range names {
		file, err := os.Open(name)
		if err != nil {
			return 0, err
		}
		defer func() { _ = file.Close() }()

		size, err := io.Copy(io.Discard, file)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}
