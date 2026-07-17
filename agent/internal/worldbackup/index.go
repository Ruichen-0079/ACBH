package worldbackup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const indexRelativePath = "world-backup/index.json"

func IndexPath(appDataDir string) string {
	return filepath.Join(appDataDir, filepath.FromSlash(indexRelativePath))
}

func LoadIndex(appDataDir string) (Index, bool, error) {
	path := IndexPath(appDataDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return emptyIndex(), false, nil
	}
	if err != nil {
		return Index{}, false, fmt.Errorf("read world backup index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, false, fmt.Errorf("parse world backup index: %w", err)
	}
	if idx.Version != IndexVersion {
		return Index{}, false, fmt.Errorf("unsupported world backup index version %d", idx.Version)
	}
	if idx.Files == nil {
		idx.Files = map[string]IndexedFile{}
	}
	return idx, true, nil
}

func SaveIndexAtomic(appDataDir string, idx Index) error {
	idx.Version = IndexVersion
	if idx.Files == nil {
		idx.Files = map[string]IndexedFile{}
	}
	path := IndexPath(appDataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create world backup index directory: %w", err)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("encode world backup index: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".index-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary index: %w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary index: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary index: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace world backup index: %w", err)
	}
	keep = true
	syncDir(filepath.Dir(path))
	return nil
}

func IndexFromManifest(m Manifest, files map[string]IndexedFile) Index {
	idx := Index{
		Version:          IndexVersion,
		LatestSnapshotID: m.SnapshotID,
		Files:            map[string]IndexedFile{},
	}
	for _, file := range m.Files {
		if indexed, ok := files[file.Path]; ok {
			idx.Files[file.Path] = indexed
			continue
		}
		idx.Files[file.Path] = IndexedFile{
			Size:     file.Size,
			SHA256:   file.SHA256,
			ObjectID: file.ObjectID,
		}
	}
	return idx
}

func emptyIndex() Index {
	return Index{
		Version: IndexVersion,
		Files:   map[string]IndexedFile{},
	}
}

func syncDir(dir string) {
	handle, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = handle.Sync()
	_ = handle.Close()
}
