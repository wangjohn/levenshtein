//go:build darwin || linux

package verify

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// cleanupProcess kills whatever is left of the command's process group once it
// has exited, whatever the outcome, not only on timeout or cancellation. A
// background process left behind could otherwise keep changing the working
// tree after the verdict, and after the result is cached. A tool that starts a
// real daemon moves it to its own session, out of this group, and keeps it.
func cleanupProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
