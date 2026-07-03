package logger

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestDBHandlerOutputsToBase(t *testing.T) {
	var buf bytes.Buffer
	h := NewDBHandler(nil, "node1", &buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(h)
	logger.Error("test error", "key", "value")
	if !bytes.Contains(buf.Bytes(), []byte("test error")) {
		t.Fatalf("expected base handler output")
	}
}
