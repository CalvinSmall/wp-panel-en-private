package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveSiteLogDirRemovesEmptyTargetCreatedByPoolApply(t *testing.T) {
	root := t.TempDir()
	oldLogDir := filepath.Join(root, "old.example.com")
	newLogDir := filepath.Join(root, "new.example.com")

	if err := os.MkdirAll(oldLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldLogDir, "access.log"), []byte("old log"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newLogDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := moveSiteLogDir(oldLogDir, newLogDir); err != nil {
		t.Fatalf("moveSiteLogDir failed: %v", err)
	}

	if _, err := os.Stat(oldLogDir); !os.IsNotExist(err) {
		t.Fatalf("old log dir still exists or stat failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(newLogDir, "access.log"))
	if err != nil {
		t.Fatalf("new log file missing: %v", err)
	}
	if string(got) != "old log" {
		t.Fatalf("new log content = %q, want old log", string(got))
	}
}

func TestMoveSiteLogDirRejectsNonEmptyTarget(t *testing.T) {
	root := t.TempDir()
	oldLogDir := filepath.Join(root, "old.example.com")
	newLogDir := filepath.Join(root, "new.example.com")

	if err := os.MkdirAll(oldLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldLogDir, "access.log"), []byte("old log"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newLogDir, "access.log"), []byte("new log"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := moveSiteLogDir(oldLogDir, newLogDir); err == nil {
		t.Fatal("expected non-empty target log dir to be rejected")
	}
	if _, err := os.Stat(filepath.Join(oldLogDir, "access.log")); err != nil {
		t.Fatalf("old log file should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newLogDir, "access.log")); err != nil {
		t.Fatalf("target log file should remain: %v", err)
	}
}
