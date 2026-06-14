package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(category, name string) string {
	return filepath.Join("testdata", category, name)
}

func TestFixturesValid(t *testing.T) {
	fixtures := []string{
		"world-snapshot.json",
		"server-pack.json",
		"admin-state.json",
		"with-tombstones.json",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			m, err := LoadFile(fixturePath("valid", name))
			if err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
			if m.ArtifactID == "" {
				t.Error("expected non-empty artifactId")
			}
		})
	}
}

func TestFixturesInvalid(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		msg      string
	}{
		{"bad version", "bad-version.json", "manifestVersion"},
		{"bad kind", "bad-kind.json", "artifactKind"},
		{"path escape", "path-escape.json", "path"},
		{"bad hash", "bad-hash.json", "sha256"},
		{"bad tombstone", "bad-tombstone.json", "deleted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(fixturePath("invalid", tt.filename))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			_, err = UnmarshalAndValidate(data)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.msg)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.msg)) {
				t.Errorf("error = %v, want message containing %q", err, tt.msg)
			}
		})
	}
}
