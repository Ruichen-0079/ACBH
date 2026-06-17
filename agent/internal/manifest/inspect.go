package manifest

import "github.com/Ruichen-0079/ACBH/agent/internal/fileclass"

type Inspection struct {
	ManifestVersion   int                         `json:"manifestVersion"`
	ArtifactKind      ArtifactKind                `json:"artifactKind"`
	ArtifactID        string                      `json:"artifactId"`
	GroupID           string                      `json:"groupId"`
	CreatorHostID     string                      `json:"creatorHostId"`
	ServerPackVersion *string                     `json:"serverPackVersion,omitempty"`
	CreatedAt         string                      `json:"createdAt"`
	FileCount         int                         `json:"fileCount"`
	DeletedCount      int                         `json:"deletedCount"`
	TotalBytes        int64                       `json:"totalBytes"`
	ClassCounts       map[fileclass.FileClass]int `json:"classCounts"`
}

func Inspect(m Manifest) (Inspection, error) {
	if err := Validate(m); err != nil {
		return Inspection{}, err
	}

	out := Inspection{
		ManifestVersion:   m.ManifestVersion,
		ArtifactKind:      m.ArtifactKind,
		ArtifactID:        m.ArtifactID,
		GroupID:           m.GroupID,
		CreatorHostID:     m.CreatorHostID,
		ServerPackVersion: m.ServerPackVersion,
		CreatedAt:         m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		FileCount:         len(m.Files),
		TotalBytes:        m.Summary.TotalBytes,
		ClassCounts:       make(map[fileclass.FileClass]int),
	}
	for _, file := range m.Files {
		out.ClassCounts[file.Class]++
		if file.Deleted {
			out.DeletedCount++
		}
	}

	return out, nil
}
