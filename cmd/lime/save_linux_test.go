package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSettingsPreserveLinuxACL(t *testing.T) {
	setfacl, err := exec.LookPath("setfacl")
	if err != nil {
		t.Skip("setfacl unavailable")
	}
	dir := t.TempDir()
	path := settingsPath(dir)
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(setfacl, "-m", "u:65534:r--", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	readACL := func() string {
		t.Helper()
		out, err := exec.Command("getfacl", "-cpn", filepath.Clean(path)).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		return string(out)
	}
	before := readACL()
	if err := writeSettingsAtomic(dir, []byte("{ }")); err != nil {
		t.Fatal(err)
	}
	if after := readACL(); after != before {
		t.Fatalf("ACL changed from %q to %q", before, after)
	}
}
