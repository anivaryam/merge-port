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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
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
