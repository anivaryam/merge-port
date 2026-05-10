package lifecycle

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	StatusStarting = "starting"
	StatusRunning  = "running"
)

type State struct {
	PID       int       `json:"pid,omitempty"`
	Port      int       `json:"port"`
	LogFile   string    `json:"log_file"`
	Args      []string  `json:"args,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Status    string    `json:"status"`
}

func StateDir(goos, home, xdg, temp string) string {
	switch goos {
	case "linux":
		if xdg != "" {
			return filepath.Join(xdg, "merge-port")
		}
		if home != "" {
			return filepath.Join(home, ".local", "state", "merge-port")
		}
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Application Support", "merge-port")
		}
	}
	return filepath.Join(temp, "merge-port-state")
}

func StatePath(dir string, port int) string {
	return filepath.Join(dir, "port-"+itoa(port)+".json")
}

func Reserve(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(state)
}

func WriteAtomic(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(state); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Read(path string) (State, error) {
	f, err := os.Open(path)
	if err != nil {
		return State{}, err
	}
	defer f.Close()
	var state State
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return State{}, err
	}
	return state, nil
}

func Remove(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func IsStartingStale(state State, now time.Time) bool {
	return state.Status == StatusStarting && now.Sub(state.StartedAt) > 30*time.Second
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	n := i
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
