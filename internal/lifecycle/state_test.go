package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStateDirUsesPlatformConventions(t *testing.T) {
	if got := StateDir("linux", "/home/me", "/state", "/tmp"); got != filepath.Join("/state", "merge-port") {
		t.Fatalf("linux xdg = %q", got)
	}
	if got := StateDir("linux", "/home/me", "", "/tmp"); got != filepath.Join("/home/me", ".local", "state", "merge-port") {
		t.Fatalf("linux fallback = %q", got)
	}
	if got := StateDir("darwin", "/Users/me", "", "/tmp"); got != filepath.Join("/Users/me", "Library", "Application Support", "merge-port") {
		t.Fatalf("darwin = %q", got)
	}
}

func TestReserveIsExclusiveAndStateRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port-8080.json")
	state := State{PID: 123, Port: 8080, LogFile: "/tmp/mp.log", Status: StatusStarting, StartedAt: time.Now()}
	if err := Reserve(path, state); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := Reserve(path, state); err == nil {
		t.Fatal("second Reserve should fail")
	}
	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if loaded.PID != 123 || loaded.Port != 8080 || loaded.Status != StatusStarting {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestReserveCreatesPrivateStateDirAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "state", "port-8080.json")
	state := State{PID: 123, Port: 8080, LogFile: "/tmp/mp.log", Status: StatusStarting, StartedAt: time.Now()}
	if err := Reserve(path, state); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	assertMode(t, filepath.Dir(path), 0700)
	assertMode(t, path, 0600)
}

func TestWriteAtomicCreatesPrivateStateDirAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "state", "port-8080.json")
	state := State{PID: 123, Port: 8080, LogFile: "/tmp/mp.log", Status: StatusRunning, StartedAt: time.Now()}
	if err := WriteAtomic(path, state); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	assertMode(t, filepath.Dir(path), 0700)
	assertMode(t, path, 0600)
}

func TestStartingStateStaleAfterThirtySeconds(t *testing.T) {
	now := time.Now()
	if !IsStartingStale(State{Status: StatusStarting, StartedAt: now.Add(-31 * time.Second)}, now) {
		t.Fatal("expected stale starting state")
	}
	if IsStartingStale(State{Status: StatusRunning, StartedAt: now.Add(-31 * time.Second)}, now) {
		t.Fatal("running state should not be starting-stale")
	}
}

func TestRemoveIgnoresMissingState(t *testing.T) {
	if err := Remove(filepath.Join(t.TempDir(), "missing.json")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove missing: %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
