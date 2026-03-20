package feishumarkdown

import (
	"testing"
)

func TestNormalizePrepareOptions_DefaultsChunkLimit(t *testing.T) {
	got := normalizePrepareOptions(PrepareOptions{})

	if got.MaxChunkRunes != DefaultMaxChunkRunes {
		t.Fatalf("expected default max chunk runes %d, got %d", DefaultMaxChunkRunes, got.MaxChunkRunes)
	}
}

func TestNormalizePrepareOptions_PreservesExplicitChunkLimit(t *testing.T) {
	want := PrepareOptions{MaxChunkRunes: 42}

	got := normalizePrepareOptions(want)

	if got != want {
		t.Fatalf("expected explicit options to remain unchanged, got %+v want %+v", got, want)
	}
}

func TestPrepareCodexMarkdown_OutputShapeContracts(t *testing.T) {
	input := "## heading\n\nbody"

	got, err := PrepareCodexMarkdown(input, PrepareOptions{MaxChunkRunes: 42})
	if err != nil {
		t.Fatalf("PrepareCodexMarkdown error: %v", err)
	}

	if got.Translated != input {
		t.Fatalf("expected translated output to match the already-compatible input, got %q", got.Translated)
	}
	if got.Chunks == nil {
		t.Fatalf("expected chunks slice to be initialized, got nil")
	}
	if len(got.Chunks) != 0 {
		t.Fatalf("expected no chunks in milestone 1, got %d", len(got.Chunks))
	}
}

func TestPrepareCodexMarkdown_TranslatesMarkdownForFeishu(t *testing.T) {
	input := "# Title"

	got, err := PrepareCodexMarkdown(input, PrepareOptions{})
	if err != nil {
		t.Fatalf("PrepareCodexMarkdown error: %v", err)
	}

	if got.Translated != "## Title" {
		t.Fatalf("expected translated heading output, got %q", got.Translated)
	}
}
