package artifactsync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

type VerifyReport struct {
	VerifiedFiles int
	TotalBytes    int64
}

func EnsureRestoreTarget(outputDir string, allowNonEmpty bool) error {
	absolute, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolve restore directory: %w", err)
	}
	if err := prepareRestoreRoot(absolute); err != nil {
		return err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return fmt.Errorf("read restore directory: %w", err)
	}
	if len(entries) > 0 && !allowNonEmpty {
		return fmt.Errorf("restore directory is not empty; pass --allow-non-empty to permit replacing files inside it")
	}
	return nil
}

func VerifyRestoredFiles(outputDir string, expected manifest.Manifest) (VerifyReport, error) {
	if err := manifest.Validate(expected); err != nil {
		return VerifyReport{}, fmt.Errorf("validate manifest: %w", err)
	}
	absolute, err := filepath.Abs(outputDir)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("resolve restore directory: %w", err)
	}
	if err := prepareRestoreRoot(absolute); err != nil {
		return VerifyReport{}, err
	}

	report := VerifyReport{}
	for _, file := range expected.Files {
		if file.Deleted {
			continue
		}
		target, err := resolveRestoreTarget(absolute, file.Path)
		if err != nil {
			return VerifyReport{}, err
		}
		parentsExist, err := checkExistingRestoreParentDirectories(absolute, target)
		if err != nil {
			return VerifyReport{}, fmt.Errorf("verify %s: %w", file.Path, err)
		}
		if !parentsExist {
			return VerifyReport{}, fmt.Errorf("verify %s: file is missing", file.Path)
		}
		exists, err := checkRestoreFinalPath(target)
		if err != nil {
			return VerifyReport{}, fmt.Errorf("verify %s: %w", file.Path, err)
		}
		if !exists {
			return VerifyReport{}, fmt.Errorf("verify %s: file is missing", file.Path)
		}
		actualSHA, actualSize, err := hashFile(target)
		if err != nil {
			return VerifyReport{}, fmt.Errorf("verify %s: %w", file.Path, err)
		}
		if actualSize != file.Size {
			return VerifyReport{}, fmt.Errorf("verify %s: size mismatch: manifest=%d actual=%d", file.Path, file.Size, actualSize)
		}
		if actualSHA != file.SHA256 {
			return VerifyReport{}, fmt.Errorf("verify %s: sha256 mismatch", file.Path)
		}
		report.VerifiedFiles++
		report.TotalBytes += actualSize
	}
	return report, nil
}
