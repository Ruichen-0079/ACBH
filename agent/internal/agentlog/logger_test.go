package agentlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterRotatesAndRedacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.jsonl")
	writer, err := New(path, 180, 3)
	if err != nil {
		t.Fatal(err)
	}
	secret := "full-access-token"
	for index := 0; index < 20; index++ {
		if err := writer.Write(Record{
			Level: "info", Event: "state_transition", Component: "relay",
			Message: "connection failed token=" + secret + " padding padding padding",
		}, secret); err != nil {
			t.Fatal(err)
		}
	}
	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		data, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatalf("expected rotated file %s: %v", candidate, err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("secret leaked in %s", candidate)
		}
		if !strings.Contains(string(data), "[REDACTED]") {
			t.Fatalf("redaction marker missing in %s", candidate)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("writer kept more than three files: %v", err)
	}
}
