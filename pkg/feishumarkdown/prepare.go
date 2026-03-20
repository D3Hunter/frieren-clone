package feishumarkdown

// DefaultMaxChunkRunes is the default rune limit used when chunk sizing is not
// provided by the caller.
const DefaultMaxChunkRunes = 1380

// PrepareOptions controls how PrepareCodexMarkdown prepares translated output.
type PrepareOptions struct {
	// MaxChunkRunes sets the intended rune budget for chunking.
	MaxChunkRunes int
}

// Chunk represents one prepared output chunk.
type Chunk struct {
	// Index is the 1-based position of this chunk.
	Index int
	// Total is the total number of chunks in the prepared output.
	Total int
	// Content contains the chunk body to send.
	Content string
}

// PreparedOutput contains the translated markdown and prepared chunks.
type PreparedOutput struct {
	// Translated is the Feishu-ready markdown translation.
	Translated string
	// Chunks contains ordered chunk payloads for delivery.
	Chunks []Chunk
}

// PrepareCodexMarkdown prepares Codex markdown for Feishu delivery.
func PrepareCodexMarkdown(input string, opts PrepareOptions) (PreparedOutput, error) {
	opts = normalizePrepareOptions(opts)

	translated, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		return PreparedOutput{}, err
	}

	rawChunks := splitMarkdownChunks(translated, opts.MaxChunkRunes)
	chunks := make([]Chunk, 0, len(rawChunks))
	for i, chunk := range rawChunks {
		if len(rawChunks) > 1 {
			chunk = withChunkSuffix(chunk, i, len(rawChunks))
		}
		chunks = append(chunks, Chunk{
			Index:   i + 1,
			Total:   len(rawChunks),
			Content: chunk,
		})
	}

	return PreparedOutput{
		Translated: translated,
		Chunks:     chunks,
	}, nil
}

func normalizePrepareOptions(opts PrepareOptions) PrepareOptions {
	if opts.MaxChunkRunes <= 0 {
		// Callers can omit the chunk cap and still get the sender-compatible default budget.
		opts.MaxChunkRunes = DefaultMaxChunkRunes
	}
	return opts
}
