package worldbackup

import (
	"context"
	"fmt"
	"io"
	"os"
)

// WorldObjectUploadFunc uploads one world CAS object. The caller retains ownership of
// content; the uploader must not close the reader.
type WorldObjectUploadFunc func(ctx context.Context, sha256 string, content io.Reader, size int64) error

// UploadMissingObjects opens each changed file, uploads missing CAS objects, and always
// closes the file handle. Upload errors are returned after closing the file.
func UploadMissingObjects(
	ctx context.Context,
	upload WorldObjectUploadFunc,
	missing []PlannedObject,
	bySHA map[string]ChangedFile,
) error {
	for _, object := range missing {
		changed, ok := bySHA[object.SHA256]
		if !ok {
			return fmt.Errorf("coordinator requested unknown object %s", object.SHA256)
		}
		file, err := os.Open(changed.LocalPath)
		if err != nil {
			return fmt.Errorf("open changed file %s: %w", changed.Path, err)
		}
		uploadErr := upload(ctx, object.SHA256, file, object.Size)
		closeErr := file.Close()
		if uploadErr != nil {
			return uploadErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// IndexChangedFilesBySHA deduplicates changed files by SHA256 for upload planning.
func IndexChangedFilesBySHA(changed []ChangedFile) map[string]ChangedFile {
	bySHA := make(map[string]ChangedFile, len(changed))
	for _, item := range changed {
		if _, ok := bySHA[item.SHA256]; !ok {
			bySHA[item.SHA256] = item
		}
	}
	return bySHA
}