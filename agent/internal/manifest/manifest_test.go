package manifest

import (
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
