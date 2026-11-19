package main

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

func TestSavePreservesACLWriteAccess(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "acl")
	if err := os.WriteFile(p, nil, 0444); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("chmod", "+a", me.Username+" allow write", p).CombinedOutput(); err != nil {
		t.Skipf("ACL unavailable: %s", out)
	}
	for _, text := range []string{"first", "second"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeFileAtomic(p, []byte(text), fi); err != nil {
			t.Fatal(err)
		}
	}
}
