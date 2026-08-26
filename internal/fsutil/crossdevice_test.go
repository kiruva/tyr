package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCrossDevice(t *testing.T) {
	if CrossDevice(nil) {
		t.Error("no error is not a cross-device error")
	}
	if CrossDevice(errors.New("something else")) {
		t.Error("an unrelated error was read as cross-device")
	}

	// A rename that fails for a different reason must not look cross-device,
	// or the caller would copy where it should report.
	dir := t.TempDir()
	err := os.Rename(filepath.Join(dir, "missing"), filepath.Join(dir, "target"))
	if err == nil {
		t.Fatal("renaming a missing file succeeded")
	}
	if CrossDevice(err) {
		t.Errorf("%v was read as cross-device", err)
	}
}
