package worldbackup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type trackingReader struct {
	*os.File
	closed bool
}

func (r *trackingReader) Close() error {
	r.closed = true
	return r.File.Close()
}

func TestUploadMissingObjectsCallerClosesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chunk.dat")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tracked := &trackingReader{File: file}

	uploaded := false
	uploadFn := func(ctx context.Context, sha256 string, content io.Reader, size int64) error {
		if _, err := io.Copy(io.Discard, content); err != nil {
			return err
		}
		if tracked.closed {
			t.Fatal("upload function closed caller-owned reader")
		}
		uploaded = true
		return nil
	}

	changed := map[string]ChangedFile{
		"abc": {Path: "world/chunk.dat", LocalPath: path, SHA256: "abc", Size: 7},
	}
	if err := UploadMissingObjects(context.Background(), uploadFn, []PlannedObject{{SHA256: "abc", Size: 7}}, changed); err != nil {
		t.Fatalf("UploadMissingObjects() error = %v", err)
	}
	if !uploaded {
		t.Fatal("expected upload to run")
	}
	if tracked.closed {
		t.Fatal("reader closed before caller Close()")
	}
	if err := tracked.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !tracked.closed {
		t.Fatal("caller Close() did not run")
	}
}

func TestUploadMissingObjectsClosesOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chunk.dat")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	uploadFn := func(ctx context.Context, sha256 string, content io.Reader, size int64) error {
		return errors.New("upload failed")
	}
	changed := map[string]ChangedFile{
		"abc": {Path: "world/chunk.dat", LocalPath: path, SHA256: "abc", Size: 7},
	}
	err := UploadMissingObjects(context.Background(), uploadFn, []PlannedObject{{SHA256: "abc", Size: 7}}, changed)
	if err == nil || !strings.Contains(err.Error(), "upload failed") {
		t.Fatalf("UploadMissingObjects() error = %v, want upload failed", err)
	}
}