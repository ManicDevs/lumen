package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewLogger_TextFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", "info")
	logger.Info("hello world")
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("expected log output to contain message, got %q", buf.String())
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", "info")
	logger.Info("test message")
	if !strings.Contains(buf.String(), `"msg":"test message"`) {
		t.Errorf("expected JSON log output, got %q", buf.String())
	}
}

func TestNewLogger_NilWriter(t *testing.T) {
	t.Parallel()
	logger := NewLogger(nil, "text", "info")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNewLogger_Levels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		level string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"warning", "warning"},
		{"error", "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(&buf, "text", tt.level)
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
		})
	}
}

func TestNewLogger_DefaultLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", "unknown")
	if logger == nil {
		t.Fatal("expected non-nil logger with unknown level")
	}
}

func TestNewLogger_DefaultFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := NewLogger(&buf, "unknown", "info")
	logger.Info("test")
	if !strings.Contains(buf.String(), "test") {
		t.Errorf("expected default text format, got %q", buf.String())
	}
}
