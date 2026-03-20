package feishumarkdown

import (
	"strings"
	"testing"
	"unicode/utf8"
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
	if len(got.Chunks) != 1 {
		t.Fatalf("expected one prepared chunk for short markdown, got %d", len(got.Chunks))
	}
	if got.Chunks[0].Index != 1 || got.Chunks[0].Total != 1 {
		t.Fatalf("expected chunk metadata {Index:1 Total:1}, got %+v", got.Chunks[0])
	}
	if got.Chunks[0].Content != input {
		t.Fatalf("expected chunk content to match translated markdown, got %q", got.Chunks[0].Content)
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

func TestPrepareCodexMarkdown_DefaultChunkBudgetKeepsPreparedChunksWithinInteractiveSafetyLimit(t *testing.T) {
	const safeInteractiveMarkdownLimit = 1400

	input := "## Markdown Playground (2K+ Characters)\n\n" +
		strings.Repeat("Markdown rendering should remain readable even with long-form text. ", 90)

	got, err := PrepareCodexMarkdown(input, PrepareOptions{})
	if err != nil {
		t.Fatalf("PrepareCodexMarkdown error: %v", err)
	}
	if len(got.Chunks) < 2 {
		t.Fatalf("expected long markdown to split into multiple chunks, got %d", len(got.Chunks))
	}

	for i, chunk := range got.Chunks {
		if gotRunes := utf8.RuneCountInString(chunk.Content); gotRunes > safeInteractiveMarkdownLimit {
			t.Fatalf("chunk %d exceeds safe interactive markdown size: %d > %d; chunk=%q", i, gotRunes, safeInteractiveMarkdownLimit, chunk.Content)
		}
	}
}
