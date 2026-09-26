package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerEmitsLokiJSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLogger(dir, "info")
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello", map[string]any{"k": "v"})
	b, _ := os.ReadFile(filepath.Join(dir, "app.log.jsonl"))
	line := strings.TrimSpace(string(b))
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if entry["msg"] != "hello" || entry["level"] != "info" || entry["k"] != "v" {
		t.Fatalf("bad entry: %v", entry)
	}
	if _, ok := entry["ts"]; !ok {
		t.Fatal("missing ts")
	}
}
