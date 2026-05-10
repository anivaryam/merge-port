//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

var errProcessStillAlive = errors.New("process still alive")

func setDetachAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func processExists(pid int) (bool, bool, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, false, err
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true, false, nil
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		return true, true, nil
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return false, false, nil
	}
	return false, false, err
}

func terminateProcess(pid int, force bool) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		exists, _, err := processExists(pid)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !force {
		return errors.New("process did not stop within 5s; retry with --force")
	}
	if err := process.Kill(); err != nil {
		return err
	}
	if err := waitForProcessExit(pid, 2*time.Second, 50*time.Millisecond, processExists); err != nil {
		if errors.Is(err, errProcessStillAlive) {
			return fmt.Errorf("process did not stop after SIGKILL; state retained for port cleanup: %w", err)
		}
		return err
	}
	return nil
}

func waitForProcessExit(pid int, timeout, interval time.Duration, exists func(int) (bool, bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		running, _, err := exists(pid)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errProcessStillAlive
		}
		time.Sleep(interval)
	}
}
