//go:build windows

package frprelay

import (
	"os"
	"testing"
)

func TestWindowsProcessIdentityUsesStableNativeFingerprint(t *testing.T) {
	pid := os.Getpid()
	if !processAlive(pid) {
		t.Fatalf("current process %d was not detected as alive", pid)
	}
	first, err := processFingerprint(pid)
	if err != nil {
		t.Fatal(err)
	}
	second, err := processFingerprint(pid)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("process fingerprint is not stable: %q != %q", first, second)
	}
}

func TestWindowsProcessIdentityRejectsInvalidPID(t *testing.T) {
	if processAlive(-1) {
		t.Fatal("invalid PID reported as alive")
	}
	if _, err := processFingerprint(-1); err == nil {
		t.Fatal("invalid PID fingerprint succeeded")
	}
}
