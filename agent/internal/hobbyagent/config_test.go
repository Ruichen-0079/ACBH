package hobbyagent

import (
	"path/filepath"
	"testing"
)

func TestConfigPortDefaultsPersistAcrossLoad(t *testing.T) {
	store := FileStore{ConfigPath: filepath.Join(t.TempDir(), "config.json")}
	input := Config{CoordinatorHost: "vps.example.test", CoordinatorPort: 6121, AccessToken: "secret"}
	if err := store.SaveConfig(input); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MinecraftLocalPort != 25565 || loaded.PublicMinecraftPort != 25565 {
		t.Fatalf("unexpected defaults: %+v", loaded)
	}
}

func TestConfigCustomPortsPersistAcrossLoad(t *testing.T) {
	store := FileStore{ConfigPath: filepath.Join(t.TempDir(), "config.json")}
	input := Config{CoordinatorHost: "vps.example.test", CoordinatorPort: 6122, AccessToken: "secret", MinecraftLocalPort: 25566, PublicMinecraftPort: 25575}
	if err := store.SaveConfig(input); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MinecraftLocalPort != 25566 || loaded.PublicMinecraftPort != 25575 {
		t.Fatalf("custom ports were not persisted: %+v", loaded)
	}
}

func TestConfigRejectsPortsOutsideUserRange(t *testing.T) {
	for _, input := range []Config{
		{CoordinatorHost: "vps", AccessToken: "secret", MinecraftLocalPort: 1023, PublicMinecraftPort: 25575},
		{CoordinatorHost: "vps", AccessToken: "secret", MinecraftLocalPort: 25566, PublicMinecraftPort: 65536},
		{CoordinatorHost: "vps", AccessToken: "secret", MinecraftLocalPort: -1, PublicMinecraftPort: 25575},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("invalid ports were accepted: %+v", input)
		}
	}
}
