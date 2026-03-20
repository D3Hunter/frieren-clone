# Feishu Markdown Library Usage

This document describes the reusable `pkg/feishumarkdown` package after Milestones 1-5.

The package is meant for Codex-style markdown that needs to be translated and chunked for Feishu interactive cards.
The primary entry point is `PrepareCodexMarkdown`.

## Public API

### `const DefaultMaxChunkRunes = 1380`

Default rune budget used when chunk sizing is omitted or set to a non-positive value.

This default matches the sender-safe interactive markdown budget used in this repository.

### `type PrepareOptions struct`

```go
type PrepareOptions struct {
    MaxChunkRunes int
}
```

`MaxChunkRunes` is the intended rune budget for each prepared chunk.

Current behavior:

- `MaxChunkRunes <= 0` is normalized to `DefaultMaxChunkRunes`.
- The suffix marker `[i/n]` is appended after splitting, so final chunk length can exceed the numeric budget slightly.
- The library does not try to compensate for suffix length ahead of time.

### `type Chunk struct`

```go
type Chunk struct {
    Index   int
    Total   int
    Content string
}
```

`Index` and `Total` are 1-based metadata values for ordered delivery.
`Content` is the exact payload to send.

### `type PreparedOutput struct`

```go
type PreparedOutput struct {
    Translated string
    Chunks     []Chunk
}
```

`Translated` is the Feishu-compatible markdown produced before chunk suffixes are added.
`Chunks` contains the final ordered send payloads.

### `func PrepareCodexMarkdown(input string, opts PrepareOptions) (PreparedOutput, error)`

This is the one-call library entry point.

It performs three steps:

1. Normalize and translate Codex markdown into Feishu-compatible markdown.
2. Split the translated markdown into safe chunks using the configured rune budget.
3. Add `[i/n]` suffix markers when more than one chunk is needed.

Errors currently come from translation. Chunk splitting is best-effort and does not return an error today.

### `func TranslateCodexMarkdownToFeishu(input string) (string, error)`

Use this when you only need the translation step and want to handle chunking yourself.

Current translation behavior includes:

- H1 headings are downgraded to H2 for Feishu visibility.
- H5/H6 headings are clamped to H4.
- Local file links are degraded to inline code.
- HTTP(S) links are preserved.
- Images are degraded to readable links or text depending on target.
- Raw HTML is escaped.
- Task lists render as readable `[x]` / `[ ]` text.
- Top-level `markdown` fences are unwrapped when they contain real markdown content.
- Large fenced markdown blocks may be recursively rendered instead of sent as a single code blob.

### `func SplitTranslatedMarkdown(input string, maxRunes int) []string`

Use this only when you already have Feishu-compatible markdown and want the splitter behavior without translation.

Current chunking behavior:

- Preserves fenced code blocks as units when possible.
- Keeps table headers with separators.
- Keeps headings attached to the following table/list/code block when possible.
- Falls back to plain text splitting for oversized blocks or when a balanced fence cannot fit inside the cap.

## Quickstart

```go
package main

import (
    "fmt"

    "github.com/D3Hunter/frieren-clone/pkg/feishumarkdown"
)

func main() {
    input := "# Title\n\n- item one\n- item two"

    prepared, err := feishumarkdown.PrepareCodexMarkdown(input, feishumarkdown.PrepareOptions{})
    if err != nil {
        panic(err)
    }

    fmt.Println(prepared.Translated)
    for _, chunk := range prepared.Chunks {
        fmt.Printf("chunk %d/%d: %q\n", chunk.Index, chunk.Total, chunk.Content)
    }
}
```

If the input already fits in one chunk, `Chunks` contains one item and the content is not suffixed.

If multiple chunks are needed, each chunk gets a trailing marker like:

```text
[1/3]
```

## Expected Outputs

### For already-compatible short markdown

Input:

```go
"## heading\n\nbody"
```

Expected shape:

- `Translated` is the translated markdown text.
- `Chunks` is initialized and contains one chunk.
- `Chunks[0].Index == 1`
- `Chunks[0].Total == 1`
- `Chunks[0].Content` matches the translated markdown exactly.

### For translated markdown that needs chunking

Input:

```go
strings.Repeat("long markdown content...", 200)
```

Expected shape:

- `Chunks` has more than one element.
- Each chunk keeps its original order.
- Each `Chunk.Content` contains a `[i/n]` suffix.
- Chunk contents are Feishu markdown, not raw Codex markdown.

### For fenced code blocks

Expected behavior:

- A fenced block is not split in the middle unless it is too large to fit.
- Balanced fences are preserved per chunk when possible.
- If the cap is too small to fit the wrapper and body together, the splitter falls back to plain text splitting to respect the cap.

### For tables

Expected behavior:

- A table header stays with its separator line.
- Table rows remain grouped with the header block when possible.

### For headings

Expected behavior:

- A heading may be carried forward with the next block if that block would otherwise start a new chunk without section context.
- This is specifically to keep Feishu cards readable when a table, list, or code block starts a chunk.

## Integration Examples

### 1. Use the package directly in another Go service

```go
prepared, err := feishumarkdown.PrepareCodexMarkdown(rawCodexMarkdown, feishumarkdown.PrepareOptions{
    MaxChunkRunes: feishumarkdown.DefaultMaxChunkRunes,
})
if err != nil {
    return err
}

for _, chunk := range prepared.Chunks {
    // Send chunk.Content as interactive markdown, in order.
    _ = chunk
}
```

### 2. Translate first, chunk later

```go
translated, err := feishumarkdown.TranslateCodexMarkdownToFeishu(rawCodexMarkdown)
if err != nil {
    return err
}

chunks := feishumarkdown.SplitTranslatedMarkdown(translated, 1380)
```

Use this path only when the caller wants to own the translation/chunking boundary separately.

### 3. Feishu sender-style integration

```go
prepared, err := feishumarkdown.PrepareCodexMarkdown(rawCodexMarkdown, feishumarkdown.PrepareOptions{
    MaxChunkRunes: 1380,
})
if err != nil {
    return err
}

for _, chunk := range prepared.Chunks {
    // Try interactive markdown first.
    // If the platform rejects it, retry the same chunk as plain text.
    sendFeishuInteractive(chunk.Content)
}
```

The repository's sender currently uses this package exactly this way, and keeps the plain-text fallback path per chunk.

## Important Caveats

- The suffix marker budget is not reserved up front. If you need a hard upper bound on final payload length, subtract a safety margin before calling `PrepareCodexMarkdown`.
- The default `1380` budget is intentionally smaller than the sender's plain-text chunk budget because interactive markdown cards are less forgiving.
- `PrepareCodexMarkdown` returns the translated string even when the caller only cares about chunks. That makes it easy to log or inspect the final markdown before sending.
- The library does not perform Feishu API calls. It only prepares content for delivery.
- The package is scoped to Codex markdown behavior. Plain-text chunking remains the caller's responsibility.

## Verification Checklist

Use this checklist when adopting the library in another project:

- [ ] Import `pkg/feishumarkdown` from the correct module path.
- [ ] Call `PrepareCodexMarkdown` for Codex markdown inputs.
- [ ] Use `PrepareOptions{}` or set `MaxChunkRunes` explicitly.
- [ ] Confirm your delivery layer sends `Chunk.Content` in order.
- [ ] Confirm your Feishu path handles interactive failure and retries as plain text if needed.
- [ ] Verify long inputs split as expected and that chunk suffixes are acceptable in your final card budget.
- [ ] Run the package and consumer tests after wiring the integration.

