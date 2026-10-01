//go:build windows

package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func cancelCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	root := os.Getenv("SystemRoot")
	if root == "" || !filepath.IsAbs(root) {
		return errors.New("Windows system directory unavailable for process-tree cancellation")
	}
	program := filepath.Join(root, "System32", "taskkill.exe")
	killer := exec.Command(program, "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	killer.Env = []string{"SystemRoot=" + root, "WINDIR=" + root}
	if err := killer.Run(); err != nil {
		if killErr := cmd.Process.Kill(); errors.Is(killErr, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return errors.New("Windows process-tree termination failed")
	}
	return nil
}
