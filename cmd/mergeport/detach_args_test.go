package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRewriteDetachArgsAbsolutizesExplicitRelativeConfig(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	got, err := rewriteDetachArgs([]string{"--config", "dev.yaml", "--detach", "--silent", "--log-file", "old.log"}, "new.log")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--config", filepath.Join(dir, "dev.yaml"), "--silent", "--log-file", "new.log"}
	got = normalizeConfigArgsForCompare(t, got)
	want = normalizeConfigArgsForCompare(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestRewriteDetachArgsInjectsImplicitCWDConfig(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".merge-port.yaml", []byte("client: 3000\nserver: 3001\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := rewriteDetachArgs([]string{"--client", "3000", "--detach"}, "new.log")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--client", "3000", "--config", filepath.Join(dir, ".merge-port.yaml"), "--silent", "--log-file", "new.log"}
	got = normalizeConfigArgsForCompare(t, got)
	want = normalizeConfigArgsForCompare(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func normalizeConfigArgsForCompare(t *testing.T, args []string) []string {
	t.Helper()
	out := append([]string(nil), args...)
	for i := 1; i < len(out); i++ {
		if out[i-1] == "--config" {
			out[i] = normalizePathForCompare(t, out[i])
		}
	}
	return out
}

func normalizePathForCompare(t *testing.T, path string) string {
	t.Helper()
	dir, file := filepath.Split(path)
	evaluatedDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(evaluatedDir, file)
}

func TestRewriteDetachArgsDoesNotInjectMissingImplicitConfig(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	got, err := rewriteDetachArgs([]string{"--client", "3000", "--detach"}, "new.log")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--client", "3000", "--silent", "--log-file", "new.log"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}
