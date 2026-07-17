package worldbackup

import "testing"

func TestNormalizeManifestPathRejectsTraversal(t *testing.T) {
	for _, raw := range []string{
		"../secret",
		"..",
		"",
		"C:\\world\\level.dat",
		"/etc/passwd",
	} {
		if _, err := NormalizeManifestPath(raw); err == nil {
			t.Fatalf("NormalizeManifestPath(%q) should fail", raw)
		}
	}
}

func TestNormalizeManifestPathAcceptsWindowsSeparators(t *testing.T) {
	got, err := NormalizeManifestPath(`world\region\r.0.0.mca`)
	if err != nil {
		t.Fatalf("NormalizeManifestPath() error = %v", err)
	}
	if got != "world/region/r.0.0.mca" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeManifestPathDedupesDots(t *testing.T) {
	got, err := NormalizeManifestPath("world/./region/r.0.0.mca")
	if err != nil {
		t.Fatalf("NormalizeManifestPath() error = %v", err)
	}
	if got != "world/region/r.0.0.mca" {
		t.Fatalf("got %q", got)
	}
}