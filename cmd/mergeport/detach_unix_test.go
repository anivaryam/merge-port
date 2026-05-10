//go:build !windows

package main

import (
	"errors"
	"testing"
	"time"
)

func TestWaitForProcessExitReturnsErrorWhenStillAlive(t *testing.T) {
	checks := 0
	err := waitForProcessExit(123, 20*time.Millisecond, time.Millisecond, func(pid int) (bool, bool, error) {
		checks++
		if pid != 123 {
			t.Fatalf("pid = %d", pid)
		}
		return true, false, nil
	})
	if err == nil {
		t.Fatal("expected still-alive error")
	}
	if !errors.Is(err, errProcessStillAlive) {
		t.Fatalf("error = %v", err)
	}
	if checks < 2 {
		t.Fatalf("expected polling, got %d checks", checks)
	}
}

func TestWaitForProcessExitReturnsNilWhenGone(t *testing.T) {
	err := waitForProcessExit(123, time.Second, time.Millisecond, func(pid int) (bool, bool, error) {
		return false, false, nil
	})
	if err != nil {
		t.Fatalf("waitForProcessExit: %v", err)
	}
}
