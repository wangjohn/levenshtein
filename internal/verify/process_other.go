//go:build !darwin && !linux

package verify

import (
	"context"
	"fmt"
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {}
func cleanupProcess(cmd *exec.Cmd)   {}
func lockFile(ctx context.Context, path string) (func(), error) {
	return nil, fmt.Errorf("cache locking requires macOS or Linux")
}
