package logging

import "testing"

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
