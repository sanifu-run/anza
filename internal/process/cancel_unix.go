//go:build !windows

package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func cancelCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return killOwnedGroup(cmd.Process.Pid)
}

func cleanupAfterWaitDelay(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return killOwnedGroup(cmd.Process.Pid)
}

func killOwnedGroup(pid int) error {
	// A same-group descendant may outlive the leader and keep our output pipe
	// open. Probe the group itself, then kill it. The probe and kill cannot be
	// atomic; a narrow PGID-reuse interval remains.
	if err := syscall.Kill(-pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}
