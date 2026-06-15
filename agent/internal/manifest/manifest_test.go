package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
)

func TestValidateManifest(t *testing.T) {
	m := validWorldManifest()
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsMixedArtifactClass(t *testing.T) {
	m := validWorldManifest()
	m.Files[0].Class = fileclass.ServerPack

	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Validate() error = %v, want class error", err)
	}
}

func TestValidateRejectsBadPathAndHash(t *testing.T) {
	m := validWorldManifest()
	m.Files[0].Path = "../world/level.dat"
	if err := Validate(m); err == nil {
		t.Fatal("Validate() accepted traversal path")
	}

	m = validWorldManifest()
	m.Files[0].SHA256 = strings.ToUpper(m.Files[0].SHA256)
	if err := Validate(m); err == nil {
		t.Fatal("Validate() accepted uppercase sha")
	}
}

func TestValidateServerRuntimeManifest(t *testing.T) {
	generation := 4
	modifiedAt := fixedTime()
	m := Manifest{
		ManifestVersion: ManifestVersion,
		ArtifactKind:    ServerRuntime,
		ArtifactID:      "runtime_000001",
		GroupID:         "group_abc",
		CreatedAt:       fixedTime(),
		CreatorHostID:   "host_abc",
		Generation:      &generation,
		Files: []FileEntry{{
			Path:       "server.properties",
			Class:      fileclass.ServerRuntime,
			Size:       8,
			SHA256:     strings.Repeat("a", 64),
			ModifiedAt: &modifiedAt,
		}},
		Summary: Summary{IncludedFiles: 1, TotalBytes: 8},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	m.Generation = nil
	if err := Validate(m); err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatalf("Validate() error = %v, want generation rejection", err)
	}
}

func TestValidateServerRuntimeRejectsUnsafePathsAndWrongClass(t *testing.T) {
	generation := 0
	modifiedAt := fixedTime()
	base := Manifest{
		ManifestVersion: ManifestVersion,
		ArtifactKind:    ServerRuntime,
		ArtifactID:      "runtime_000001",
		GroupID:         "group_abc",
		CreatedAt:       fixedTime(),
		CreatorHostID:   "host_abc",
		Generation:      &generation,
		Files: []FileEntry{{
			Path:       "server.properties",
			Class:      fileclass.ServerRuntime,
			Size:       1,
			SHA256:     strings.Repeat("a", 64),
			ModifiedAt: &modifiedAt,
		}},
		Summary: Summary{IncludedFiles: 1, TotalBytes: 1},
	}

	for _, unsafe := range []string{"../evil", "/absolute", `C:\server\file`, "C:relative", `\\server\share\file`, "bad\x00file"} {
		m := base
		m.Files = append([]FileEntry(nil), base.Files...)
		m.Files[0].Path = unsafe
		if err := Validate(m); err == nil {
			t.Fatalf("Validate() accepted unsafe path %q", unsafe)
		}
	}

	m := base
	m.Files = append([]FileEntry(nil), base.Files...)
	m.Files[0].Class = fileclass.ServerPack
	if err := Validate(m); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Validate() error = %v, want class rejection", err)
	}
}

func TestUnmarshalRequiresSummary(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "invalid", "missing-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalAndValidate(data); err == nil || !strings.Contains(err.Error(), "summary is required") {
		t.Fatalf("UnmarshalAndValidate() error = %v, want required summary", err)
	}
}

func TestUnmarshalRejectsDriveRelativePath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "invalid", "drive-relative.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalAndValidate(data); err == nil || !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("UnmarshalAndValidate() error = %v, want drive path rejection", err)
	}
}

func TestValidateDeletedEntryUsesEmptyHash(t *testing.T) {
	m := validWorldManifest()
	m.Files = append(m.Files, FileEntry{
		Path:    "world/region/r.1.0.mca",
		Class:   fileclass.WorldRuntime,
		Size:    0,
		SHA256:  "",
		Deleted: true,
	})
	m.Summary.IncludedFiles = 1
	m.Summary.DeletedFiles = 1
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRequiresSortedFiles(t *testing.T) {
	m := validWorldManifest()
	modifiedAt := fixedTime()
	m.Files = append(m.Files, FileEntry{
		Path:       "world/aaa.dat",
		Class:      fileclass.WorldRuntime,
		Size:       1,
		SHA256:     strings.Repeat("b", 64),
		ModifiedAt: &modifiedAt,
		Deleted:    false,
	})
	m.Summary.IncludedFiles = 2
	m.Summary.TotalBytes = 1235

	if err := Validate(m); err == nil {
		t.Fatal("Validate() accepted unsorted files")
	}
}

func TestDiff(t *testing.T) {
	oldManifest := validWorldManifest()
	newManifest := validWorldManifest()
	newManifest.ArtifactID = "snap_000002"
	modifiedAt := fixedTime()
	newManifest.Files = []FileEntry{
		{
			Path:       "plugins/LuckPerms/config.yml",
			Class:      fileclass.PluginRuntimeData,
			Size:       5,
			SHA256:     strings.Repeat("c", 64),
			ModifiedAt: &modifiedAt,
		},
		{
			Path:       "world/region/r.0.0.mca",
			Class:      fileclass.WorldRuntime,
			Size:       1234,
			SHA256:     strings.Repeat("b", 64),
			ModifiedAt: &modifiedAt,
		},
		{
			Path:    "world/region/r.1.0.mca",
			Class:   fileclass.WorldRuntime,
			Size:    0,
			SHA256:  "",
			Deleted: true,
		},
	}
	newManifest.Summary = Summary{IncludedFiles: 2, DeletedFiles: 1, TotalBytes: 1239}

	got, err := Diff(oldManifest, newManifest)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if got.Added != 1 || got.Modified != 1 || got.Deleted != 1 || got.Unchanged != 0 {
		t.Fatalf("Diff() = %#v", got)
	}
}

func validWorldManifest() Manifest {
	modifiedAt := fixedTime()
	pack := "pack_000001"
	return Manifest{
		ManifestVersion:   ManifestVersion,
		ArtifactKind:      WorldSnapshot,
		ArtifactID:        "snap_000001",
		GroupID:           "group_abc",
		CreatedAt:         fixedTime(),
		CreatorHostID:     "host_abc",
		ParentArtifactID:  nil,
		ServerPackVersion: &pack,
		Files: []FileEntry{
			{
				Path:       "world/region/r.0.0.mca",
				Class:      fileclass.WorldRuntime,
				Size:       1234,
				SHA256:     strings.Repeat("a", 64),
				ModifiedAt: &modifiedAt,
				Deleted:    false,
			},
		},
		Summary: Summary{
			IncludedFiles: 1,
			IgnoredFiles:  0,
			UnknownFiles:  0,
			DeletedFiles:  0,
			TotalBytes:    1234,
		},
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
}
