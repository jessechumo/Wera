package buildinfo

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	if s := String(); !strings.HasPrefix(s, Version+" (") || Commit == "" {
		t.Errorf("String() = %q, Commit = %q", s, Commit)
	}
}
