//go:build !windows

package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func securePath(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return ErrInsecureState
	}
	return nil
}

func atomicWrite(path string, data []byte, beforeReplace func() error) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".anza-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create state temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set state temporary file mode: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write state temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("flush state temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close state temporary file: %w", err)
	}
	if beforeReplace != nil {
		if err := beforeReplace(); err != nil {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("flush state directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func lockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return ErrLocked
	}
	return fmt.Errorf("acquire operating-system file lock: %w", err)
}

func unlockFile(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	return nil
}

func filesystemIdentity(_ string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("filesystem does not expose device and inode identity")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
