package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteProfile(t *testing.T) {
	var out bytes.Buffer
	if err := writeProfile(&out, "", "# Candidate Profile"); err != nil || out.String() != "# Candidate Profile\n" {
		t.Fatalf("stdout: %q %v", out.String(), err)
	}
	path := filepath.Join(t.TempDir(), "me.md")
	if err := writeProfile(&out, path, "# P\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode: %v %v", info.Mode(), err)
	}
	if err := writeProfile(&out, path, "# Other\n"); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("overwrote an existing file: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "# P\n" {
		t.Errorf("file changed: %q", b)
	}
}
