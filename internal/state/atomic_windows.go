//go:build windows

package state

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
	lockfileFailImmediately = 0x1
	lockfileExclusive       = 0x2
	errorLockViolation      = 33
	fileFlagBackupSemantics = 0x02000000
	openExisting            = 3
	shareRead               = 0x1
	shareWrite              = 0x2
	shareDelete             = 0x4
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW        = kernel32.NewProc("MoveFileExW")
	procLockFileEx         = kernel32.NewProc("LockFileEx")
	procUnlockFileEx       = kernel32.NewProc("UnlockFileEx")
	procGetFileInformation = kernel32.NewProc("GetFileInformationByHandle")
	procCreateFileW        = kernel32.NewProc("CreateFileW")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	windowsSIDPattern      = regexp.MustCompile(`S-1-[0-9-]+`)
)

func securePath(path string, directory bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	identity, err := currentUserSID()
	if err != nil {
		return err
	}
	permission := identity + ":F"
	if directory {
		permission = identity + ":(OI)(CI)F"
	}
	// Reset inherited permissions, disable inheritance, and grant full access
	// only to the current user's SID. Failure is fatal; Windows mode bits alone
	// do not provide a private ACL.
	cmd := exec.Command("icacls", absolute, "/reset", "/inheritance:r", "/grant:r", permission)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("set owner-only Windows ACL: %w", err)
	}
	return nil
}

func currentUserSID() (string, error) {
	cmd := exec.Command("whoami", "/user", "/fo", "csv", "/nh")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve current Windows user SID: %w", err)
	}
	sid := windowsSIDPattern.Find(output)
	if len(sid) == 0 {
		return "", errors.New("whoami did not return a Windows SID")
	}
	return string(sid), nil
}

func atomicWrite(path string, data []byte, beforeReplace func() error) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".anza-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create state temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := securePath(tempPath, false); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure state temporary file: %w", err)
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
	from, err := syscall.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, callErr := procMoveFileExW.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), moveFileReplaceExisting|moveFileWriteThrough)
	if result == 0 {
		return fmt.Errorf("replace state file with write-through: %w", callErr)
	}
	return nil
}

func syncDirectory(string) error { return nil }

func lockFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := procLockFileEx.Call(uintptr(syscall.Handle(file.Fd())), lockfileExclusive|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result != 0 {
		return nil
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorLockViolation {
		return ErrLocked
	}
	return fmt.Errorf("acquire Windows file lock: %w", callErr)
}

func unlockFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := procUnlockFileEx.Call(uintptr(syscall.Handle(file.Fd())), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result == 0 {
		return fmt.Errorf("release Windows file lock: %w", callErr)
	}
	return nil
}

func filesystemIdentity(path string, _ os.FileInfo) (string, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, _, callErr := procCreateFileW.Call(uintptr(unsafe.Pointer(name)), 0, shareRead|shareWrite|shareDelete, 0, openExisting, fileFlagBackupSemantics, 0)
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return "", fmt.Errorf("open workspace identity handle: %w", callErr)
	}
	defer procCloseHandle.Call(handle)
	var info syscall.ByHandleFileInformation
	result, _, callErr := procGetFileInformation.Call(handle, uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return "", fmt.Errorf("read workspace file identity: %w", callErr)
	}
	index := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	return fmt.Sprintf("%d:%d", info.VolumeSerialNumber, index), nil
}
