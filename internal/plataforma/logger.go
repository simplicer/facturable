// Package plataforma provides shared technical plumbing: Loki-compatible
// structured logging, configuration loading from etc/ and JSON helpers.
package plataforma

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger emits one JSON object per line (Loki friendly).
type Logger struct {
	mu  sync.Mutex
	out *os.File
	lvl string
}

func NewLogger(dir, level string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "app.log.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	return &Logger{out: f, lvl: level}, nil
}

func (l *Logger) log(level, msg string, kv map[string]any) {
	if l == nil || (l.lvl == "info" && level == "debug") {
		return
	}
	entry := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano), "level": level, "msg": msg}
	for k, v := range kv {
		entry[k] = v
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(b, '\n'))
}

func (l *Logger) Info(msg string, kv map[string]any)  { l.log("info", msg, kv) }
func (l *Logger) Error(msg string, kv map[string]any) { l.log("error", msg, kv) }
func (l *Logger) Debug(msg string, kv map[string]any) { l.log("debug", msg, kv) }
