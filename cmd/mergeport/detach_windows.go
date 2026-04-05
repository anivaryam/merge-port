//go:build windows

package main

import "os/exec"

// setDetachAttr is a no-op on Windows; process inherits the parent's console group.
func setDetachAttr(cmd *exec.Cmd) {}
