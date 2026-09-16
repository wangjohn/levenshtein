//go:build !darwin && !linux

package verify

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}

func cleanupProcess(cmd *exec.Cmd) {}
