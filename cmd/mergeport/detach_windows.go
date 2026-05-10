//go:build windows

package main

import (
	"errors"
	"os/exec"
)

// setDetachAttr is a no-op on Windows; process inherits the parent's console group.
func setDetachAttr(cmd *exec.Cmd) {}

func processExists(pid int) (bool, bool, error) {
	return false, false, errors.New("--detach, status, and stop are not supported on Windows yet")
}

func terminateProcess(pid int, force bool) error {
	return errors.New("--detach, status, and stop are not supported on Windows yet")
}
