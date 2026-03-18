package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeFormatDefaultsToText(t *testing.T) {
	if got := normalizeFormat(""); got != FormatText {
		t.Fatalf("expected text format by default, got %q", got)
	}
	if got := normalizeFormat("json"); got != FormatJSON {
		t.Fatalf("expected json format, got %q", got)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	_, err := New(Options{Level: "bad-level"})
	if err == nil {
		t.Fatal("expected invalid level error")
	}
}

func TestNewDefaultsToRelativeLogFile(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	logger, err := New(Options{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	logger.Info("default log file check")
	_ = logger.Sync()

	logData, err := os.ReadFile(filepath.Join("logs", "frieren.log"))
	if err != nil {
		t.Fatalf("read default log file: %v", err)
	}
	if !strings.Contains(string(logData), "default log file check") {
		t.Fatalf("expected log file to contain test message, got %q", string(logData))
	}
}
