package coordinator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type closeTrackingFile struct {
	*os.File
	closed bool
}

func (f *closeTrackingFile) Close() error {
	f.closed = true
	return f.File.Close()
}

func TestUploadWorldObjectStreamDoesNotCloseCallerReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "object.bin")
	payload := []byte("world-object-payload")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(body) != string(payload) {
			t.Fatalf("body = %q, want %q", body, payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"sha256":"deadbeef","exists":false,"size":20}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	raw, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tracked := &closeTrackingFile{File: raw}

	sha := "deadbeef"
	if _, err := client.UploadWorldObjectStream(
		context.Background(),
		ArtifactAuth{GroupID: "grp", HostID: "host", HostToken: "token"},
		sha,
		tracked,
		int64(len(payload)),
	); err != nil {
		t.Fatalf("UploadWorldObjectStream() error = %v", err)
	}
	if tracked.closed {
		t.Fatal("UploadWorldObjectStream closed caller-owned reader")
	}
	if err := tracked.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !tracked.closed {
		t.Fatal("caller Close() did not run")
	}
}