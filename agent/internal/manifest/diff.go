package manifest

type DiffResult struct {
	ArtifactKind  ArtifactKind `json:"artifactKind"`
	OldArtifactID string       `json:"oldArtifactId"`
	NewArtifactID string       `json:"newArtifactId"`
	Added         int          `json:"added"`
	Modified      int          `json:"modified"`
	Deleted       int          `json:"deleted"`
	Unchanged     int          `json:"unchanged"`
}

func Diff(oldManifest, newManifest Manifest) (DiffResult, error) {
	if err := Validate(oldManifest); err != nil {
		return DiffResult{}, err
	}
	if err := Validate(newManifest); err != nil {
		return DiffResult{}, err
	}
	if oldManifest.ArtifactKind != newManifest.ArtifactKind {
		return DiffResult{}, ErrArtifactKindMismatch{
			Old: oldManifest.ArtifactKind,
			New: newManifest.ArtifactKind,
		}
	}

	result := DiffResult{
		ArtifactKind:  oldManifest.ArtifactKind,
		OldArtifactID: oldManifest.ArtifactID,
		NewArtifactID: newManifest.ArtifactID,
	}

	oldFiles := make(map[string]FileEntry, len(oldManifest.Files))
	for _, file := range oldManifest.Files {
		if !file.Deleted {
			oldFiles[file.Path] = file
		}
	}

	for _, file := range newManifest.Files {
		if file.Deleted {
			result.Deleted++
			delete(oldFiles, file.Path)
			continue
		}

		oldFile, ok := oldFiles[file.Path]
		if !ok {
			result.Added++
			continue
		}
		if oldFile.SHA256 == file.SHA256 {
			result.Unchanged++
		} else {
			result.Modified++
		}
		delete(oldFiles, file.Path)
	}

	result.Deleted += len(oldFiles)
	return result, nil
}

type ErrArtifactKindMismatch struct {
	Old ArtifactKind
	New ArtifactKind
}

func (err ErrArtifactKindMismatch) Error() string {
	return "manifest artifact kinds differ: old=" + string(err.Old) + " new=" + string(err.New)
}
