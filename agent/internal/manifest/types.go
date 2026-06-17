package manifest

import (
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
)

const ManifestVersion = 1

type ArtifactKind string

const (
	WorldSnapshot ArtifactKind = "world-snapshot"
	ServerPack    ArtifactKind = "server-pack"
	AdminState    ArtifactKind = "admin-state"
)

type Manifest struct {
	ManifestVersion   int          `json:"manifestVersion"`
	ArtifactKind      ArtifactKind `json:"artifactKind"`
	ArtifactID        string       `json:"artifactId"`
	GroupID           string       `json:"groupId"`
	CreatedAt         time.Time    `json:"createdAt"`
	CreatorHostID     string       `json:"creatorHostId"`
	ParentArtifactID  *string      `json:"parentArtifactId"`
	ServerPackVersion *string      `json:"serverPackVersion,omitempty"`
	Files             []FileEntry  `json:"files"`
	Summary           Summary      `json:"summary"`
}

type FileEntry struct {
	Path       string              `json:"path"`
	Class      fileclass.FileClass `json:"class"`
	Size       int64               `json:"size"`
	SHA256     string              `json:"sha256"`
	ModifiedAt *time.Time          `json:"modifiedAt,omitempty"`
	Deleted    bool                `json:"deleted"`
}

type Summary struct {
	IncludedFiles int   `json:"includedFiles"`
	IgnoredFiles  int   `json:"ignoredFiles"`
	UnknownFiles  int   `json:"unknownFiles"`
	DeletedFiles  int   `json:"deletedFiles"`
	TotalBytes    int64 `json:"totalBytes"`
}

func IsValidArtifactKind(kind ArtifactKind) bool {
	switch kind {
	case WorldSnapshot, ServerPack, AdminState:
		return true
	default:
		return false
	}
}

func ClassAllowedForArtifact(kind ArtifactKind, class fileclass.FileClass) bool {
	switch kind {
	case WorldSnapshot:
		return class == fileclass.WorldRuntime || class == fileclass.PluginRuntimeData
	case ServerPack:
		return class == fileclass.ServerPack
	case AdminState:
		return class == fileclass.AdminState
	default:
		return false
	}
}
