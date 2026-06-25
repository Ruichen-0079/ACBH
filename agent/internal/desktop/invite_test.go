package desktop

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSetupCreateInviteRequiresOwner(t *testing.T) {
	dir := t.TempDir()
	opts := Options{AppDataDir: dir}
	result, err := SetupCreateInvite(context.Background(), opts, 1800, true)
	if err != nil {
		t.Fatalf("SetupCreateInvite() error = %v", err)
	}
	if result.OK {
		t.Fatalf("SetupCreateInvite() = %#v, want ok=false", result)
	}
	if result.ErrorCode != "not_configured" {
		t.Fatalf("ErrorCode = %q, want not_configured", result.ErrorCode)
	}
}

func TestSetupListInvitesRequiresOwner(t *testing.T) {
	dir := t.TempDir()
	opts := Options{AppDataDir: dir}
	result, err := SetupListInvites(context.Background(), opts)
	if err != nil {
		t.Fatalf("SetupListInvites() error = %v", err)
	}
	if result.OK {
		t.Fatalf("SetupListInvites() = %#v, want ok=false", result)
	}
	if result.ErrorCode != "not_configured" {
		t.Fatalf("ErrorCode = %q, want not_configured", result.ErrorCode)
	}
}

func TestSetupRevokeInviteRequiresOwner(t *testing.T) {
	dir := t.TempDir()
	opts := Options{AppDataDir: dir}
	result, err := SetupRevokeInvite(context.Background(), opts, "inv_test")
	if err != nil {
		t.Fatalf("SetupRevokeInvite() error = %v", err)
	}
	if result.OK {
		t.Fatalf("SetupRevokeInvite() = %#v, want ok=false", result)
	}
	if result.ErrorCode != "not_configured" {
		t.Fatalf("ErrorCode = %q, want not_configured", result.ErrorCode)
	}
}

func TestDesktopConfigPersistsGroupName(t *testing.T) {
	dir := t.TempDir()
	opts := Options{AppDataDir: dir}
	cfg := defaultDesktopConfig()
	cfg.GroupName = "My Server"
	cfg.Group = DesktopGroupConfig{GroupID: "grp_1", Role: "owner"}
	if err := SaveDesktopConfig(opts, cfg); err != nil {
		t.Fatalf("SaveDesktopConfig() error = %v", err)
	}
	loaded, err := LoadDesktopConfig(opts)
	if err != nil {
		t.Fatalf("LoadDesktopConfig() error = %v", err)
	}
	if loaded.GroupName != "My Server" {
		t.Fatalf("GroupName = %q, want My Server", loaded.GroupName)
	}
	if loaded.Group.GroupID != "grp_1" {
		t.Fatalf("GroupID = %q, want grp_1", loaded.Group.GroupID)
	}
	_ = filepath.Join(dir, desktopConfigFileName)
}