//go:build windows

package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// Apply replaces the original executable from the downloaded helper process.
// It must only be called for the private --apply-update startup mode.
func Apply() error {
	payload, err := applyPayloadFromEnvironment()
	if err != nil {
		return fmt.Errorf("update parameters: %w", err)
	}
	self, err := os.Executable()
	if err != nil || filepath.Dir(self) != filepath.Dir(payload.Target) ||
		!strings.HasPrefix(filepath.Base(self), ".sidecast-update-") ||
		!strings.HasSuffix(self, "-helper.exe") {
		return errors.New("invalid update helper")
	}
	if err := waitForOriginalExit(payload.PID, payload.Started); err != nil {
		return fmt.Errorf("wait for Sidecast to close: %w", err)
	}
	defer os.Remove(payload.Ready)
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = os.Rename(payload.Target, payload.Backup)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = os.Remove(payload.Candidate)
			if restartErr := restartOld(payload, self); restartErr != nil {
				return fmt.Errorf("replace running executable: %v; restart old executable: %w", err, restartErr)
			}
			return fmt.Errorf("replace running executable: %w", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err := os.Rename(payload.Candidate, payload.Target); err != nil {
		if restoreErr := os.Rename(payload.Backup, payload.Target); restoreErr != nil {
			return fmt.Errorf("place update: %v; restore old executable: %w", err, restoreErr)
		}
		_ = os.Remove(payload.Candidate)
		if restartErr := restartOld(payload, self); restartErr != nil {
			return fmt.Errorf("place update: %v; restart old executable: %w", err, restartErr)
		}
		return fmt.Errorf("place update: %w", err)
	}
	process, err := launchUpdated(payload, self, payload.Ready)
	if err == nil {
		err = awaitReady(process, payload.Ready)
	}
	if err != nil {
		if process != nil {
			_ = process.Kill()
			_ = waitForProcessExit(process.Pid)
			_ = process.Release()
		}
		if moveErr := moveWithRetry(payload.Target, payload.Candidate); moveErr != nil {
			return fmt.Errorf("launch update: %v; remove failed executable: %w", err, moveErr)
		}
		if restoreErr := os.Rename(payload.Backup, payload.Target); restoreErr != nil {
			return fmt.Errorf("launch update: %v; restore old executable: %w", err, restoreErr)
		}
		_ = os.Remove(payload.Candidate)
		if restartErr := restartOld(payload, self); restartErr != nil {
			return fmt.Errorf("launch update: %v; restart old executable: %w", err, restartErr)
		}
		return fmt.Errorf("new Sidecast did not start: %w; old version restored", err)
	}
	_ = process.Release()
	_ = os.Remove(payload.Backup)
	return nil
}

func moveWithRetry(source, target string) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := os.Rename(source, target)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func restartOld(payload applyPayload, helper string) error {
	process, err := launchUpdated(payload, helper, "")
	if err != nil {
		return err
	}
	return process.Release()
}

func awaitReady(process *os.Process, marker string) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	deadline := time.Now().Add(20 * time.Second)
	for {
		result, err := windows.WaitForSingleObject(handle, 0)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return errors.New("new process exited before becoming ready")
		}
		if _, err := os.Stat(marker); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("new process did not become ready")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func waitForProcessExit(pid int) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil // The original process has already exited.
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, 120000)
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return errors.New("Sidecast did not exit within two minutes")
	}
	return nil
}

func waitForOriginalExit(pid int, started string) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return os.WriteFile(started, []byte("started"), 0600)
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err := os.WriteFile(started, []byte("started"), 0600); err != nil {
		return err
	}
	result, err := windows.WaitForSingleObject(handle, 120000)
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return errors.New("Sidecast did not exit within two minutes")
	}
	return nil
}

func waitForHelperReady(ctx context.Context, pid int, marker string) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return errors.New("installer exited before starting")
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := windows.WaitForSingleObject(handle, 0)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return errors.New("installer exited before starting")
		}
		if _, err := os.Stat(marker); err == nil {
			result, err = windows.WaitForSingleObject(handle, 0)
			if err == nil && result != windows.WAIT_OBJECT_0 {
				return nil
			}
			return errors.New("installer exited before starting")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("installer did not confirm startup")
		case <-ticker.C:
		}
	}
}

func launchUpdated(payload applyPayload, helper, ready string) (*os.Process, error) {
	command := exec.Command(payload.Target, payload.Arguments...)
	command.Dir = payload.WorkingDir
	command.Env = updatedEnvironment(helper, ready)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command.Process, nil
}
