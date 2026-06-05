package fileclass

import "testing"

func TestClassifyPath(t *testing.T) {
	tests := []struct {
		path string
		want FileClass
	}{
		{"world/region/r.0.0.mca", WorldRuntime},
		{"world_nether/DIM-1/region/r.0.0.mca", WorldRuntime},
		{"world_the_end/DIM1/region/r.0.0.mca", WorldRuntime},
		{"mods/fabric-api.jar", ServerPack},
		{"mods/nested/library.jar", ServerPack},
		{"plugins/Vault.jar", ServerPack},
		{"config/server.toml", ServerPack},
		{"defaultconfigs/foo.toml", ServerPack},
		{"kubejs/server_scripts/main.js", ServerPack},
		{"scripts/start.sh", ServerPack},
		{"server.properties", AdminState},
		{"whitelist.json", AdminState},
		{"ops.json", AdminState},
		{"banned-players.json", AdminState},
		{"banned-ips.json", AdminState},
		{"usercache.json", AdminState},
		{"plugins/Essentials/userdata/player.yml", PluginRuntimeData},
		{"plugins/Essentials/data/store.db", PluginRuntimeData},
		{"plugins/LuckPerms/config.yml", PluginRuntimeData},
		{"logs/latest.log", Ignored},
		{"crash-reports/crash.txt", Ignored},
		{"cache/temp.bin", Ignored},
		{"world/session.lock", Ignored},
		{"debug.log", Ignored},
		{"README.txt", Unknown},
	}

	for _, tt := range tests {
		got, err := ClassifyPath(tt.path)
		if err != nil {
			t.Fatalf("ClassifyPath(%q) error = %v", tt.path, err)
		}
		if got != tt.want {
			t.Fatalf("ClassifyPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestNormalizePathRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{
		"",
		"/world/region/r.0.0.mca",
		"C:\\servers\\world\\level.dat",
		"../world/level.dat",
		"world/../../secret",
		"world/\x00level.dat",
	} {
		if _, err := NormalizePath(path); err == nil {
			t.Fatalf("NormalizePath(%q) succeeded, want error", path)
		}
	}
}

func TestNormalizePathUsesSlashes(t *testing.T) {
	got, err := NormalizePath(`world\region\r.0.0.mca`)
	if err != nil {
		t.Fatalf("NormalizePath() error = %v", err)
	}
	if got != "world/region/r.0.0.mca" {
		t.Fatalf("NormalizePath() = %q", got)
	}
}
