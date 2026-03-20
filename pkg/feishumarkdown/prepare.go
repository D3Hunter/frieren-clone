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

	return PreparedOutput{
		Translated: input,
		Chunks:     make([]Chunk, 0),
	}, nil
}

func normalizePrepareOptions(opts PrepareOptions) PrepareOptions {
	if opts.MaxChunkRunes <= 0 {
		// Milestone 1 keeps the package surface stable by normalizing the
		// default chunk budget even though chunking is not implemented yet.
		opts.MaxChunkRunes = DefaultMaxChunkRunes
	}
	return opts
}
