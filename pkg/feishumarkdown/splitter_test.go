package feishumarkdown

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPrepareCodexMarkdown_BuildsOrderedMarkdownChunks(t *testing.T) {
	input := "# Markdown Capability Demo (H1, Level 1)\n\n" +
		strings.Repeat("Markdown chunking should preserve heading rendering fidelity. ", 120)

	got, err := PrepareCodexMarkdown(input, PrepareOptions{MaxChunkRunes: 680})
	if err != nil {
		t.Fatalf("PrepareCodexMarkdown error: %v", err)
	}
	if len(got.Chunks) < 2 {
		t.Fatalf("expected prepared markdown to split into multiple chunks, got %d", len(got.Chunks))
	}

	first := got.Chunks[0]
	if first.Index != 1 {
		t.Fatalf("expected first chunk index 1, got %d", first.Index)
	}
	if first.Total != len(got.Chunks) {
		t.Fatalf("expected first chunk total %d, got %d", len(got.Chunks), first.Total)
	}
	if !strings.HasPrefix(strings.TrimSpace(first.Content), "## Markdown Capability Demo (H1, Level 1)") {
		t.Fatalf("expected first chunk to start with translated heading, got %q", first.Content)
	}
	if !strings.Contains(first.Content, "\n\n[1/") {
		t.Fatalf("expected first chunk ordering marker as suffix, got %q", first.Content)
	}
}

func TestSplitMarkdownChunks_DoesNotSplitInsideFencedCodeBlock(t *testing.T) {
	input := strings.Join([]string{
		"intro",
		"",
		"```go",
		`fmt.Println("hello")`,
		`fmt.Println("world")`,
		"```",
		"",
		"tail " + strings.Repeat("x", 80),
	}, "\n")

	chunks := splitMarkdownChunks(input, 70)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.Count(chunk, "```")%2 != 0 {
			t.Fatalf("chunk %d contains unmatched code fence: %q", i, chunk)
		}
	}
	assertChunksWithinMaxRunes(t, chunks, 70)
}

func TestSplitMarkdownChunks_OversizedFencedBlockKeepsBalancedFencesPerChunk(t *testing.T) {
	bodyLines := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("line-%03d: %s", i, strings.Repeat("x", 18)))
	}
	input := strings.Join([]string{
		"```go",
		strings.Join(bodyLines, "\n"),
		"```",
	}, "\n")

	chunks := splitMarkdownChunks(input, 220)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized fenced block to split into multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.Count(chunk, "```")%2 != 0 {
			t.Fatalf("chunk %d contains unmatched code fence: %q", i, chunk)
		}
	}
	assertChunksWithinMaxRunes(t, chunks, 220)
}

func TestSplitMarkdownChunks_OversizedFencedBlockWithTinyCapStillRespectsChunkLimit(t *testing.T) {
	input := strings.Join([]string{
		"```go",
		"x",
		"```",
	}, "\n")

	chunks := splitMarkdownChunks(input, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected tiny cap to force fallback splitting, got %d chunks", len(chunks))
	}

	assertChunksWithinMaxRunes(t, chunks, 10)
}

func TestSplitMarkdownBlocks_FenceCloserRequiresWhitespaceSuffixOnly(t *testing.T) {
	input := strings.Join([]string{
		"```markdown",
		"# literal examples",
		"```json",
		`{"ok": true}`,
		"```",
		"after",
	}, "\n")

	blocks := splitMarkdownBlocks(input)
	if len(blocks) < 2 {
		t.Fatalf("expected fenced block and trailing paragraph blocks, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "```json\n{\"ok\": true}\n```") {
		t.Fatalf("expected fence content line with language suffix to remain inside fenced block, got %q", blocks[0])
	}
	if strings.Contains(blocks[0], "after") {
		t.Fatalf("expected trailing paragraph outside fenced block, got %q", blocks[0])
	}
}

func TestSplitMarkdownChunks_DoesNotSplitTableHeaderFromSeparator(t *testing.T) {
	input := strings.Join([]string{
		"# report",
		"",
		"| name | score |",
		"| --- | --- |",
		"| alpha | 1 |",
		"| beta | 2 |",
		"",
		"appendix " + strings.Repeat("tail ", 30),
	}, "\n")

	chunks := splitMarkdownChunks(input, 75)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.Contains(chunk, "| --- | --- |") && !strings.Contains(chunk, "| name | score |") {
			t.Fatalf("chunk %d split table header from separator: %q", i, chunk)
		}
	}
	assertChunksWithinMaxRunes(t, chunks, 75)
}

func TestSplitMarkdownChunks_KeepsSectionHeadingWithFollowingTableBlock(t *testing.T) {
	intro := strings.Repeat("Intro paragraph content to consume chunk budget. ", 12)
	input := strings.Join([]string{
		intro,
		"",
		"## Table Alignment Test",
		"",
		"| Left align | Center align | Right align |",
		"|:---|:---:|---:|",
		"| apple | red | 10 |",
		"| banana | yellow | 200 |",
		"| cherry | dark red | 3000 |",
		"",
		"tail",
	}, "\n")

	chunks := splitMarkdownChunks(input, 680)
	if len(chunks) < 2 {
		t.Fatalf("expected split into multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		hasTable := strings.Contains(chunk, "| Left align | Center align | Right align |") ||
			strings.Contains(chunk, "|:---|:---:|---:|")
		if !hasTable {
			continue
		}
		if !strings.Contains(chunk, "## Table Alignment Test") {
			t.Fatalf("expected table chunk %d to include its section heading, got %q", i, chunk)
		}
	}
	assertChunksWithinMaxRunes(t, chunks, 680)
}

func TestChooseChunkCut_UsesUnicodeWhitespaceFallback(t *testing.T) {
	input := []rune("alpha\u00a0beta gamma")

	got := chooseChunkCut(input, 8)

	if got != 6 {
		t.Fatalf("expected cut at unicode whitespace boundary, got %d", got)
	}
}

func assertChunksWithinMaxRunes(t *testing.T, chunks []string, maxRunes int) {
	t.Helper()

	for i, chunk := range chunks {
		if got := utf8.RuneCountInString(chunk); got > maxRunes {
			t.Fatalf("chunk %d exceeds max runes: %d > %d; chunk=%q", i, got, maxRunes, chunk)
		}
	}
}
