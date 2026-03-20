# Feishu Markdown Library Integration Prompt

Copy and paste the prompt below into another coding agent that is working inside a target Go project.

```text
You are working inside a Go project that needs to prepare Codex-style markdown for Feishu interactive cards.

Task:
Integrate the reusable Feishu markdown library so the project translates raw Codex markdown into Feishu-safe markdown, chunks it into ordered send-ready payloads, and preserves the current delivery behavior of the target project.

Library surface to use:
- PrepareCodexMarkdown(input string, opts PrepareOptions) (PreparedOutput, error)
- TranslateCodexMarkdownToFeishu(input string) (string, error)
- SplitTranslatedMarkdown(input string, maxRunes int) []string
- DefaultMaxChunkRunes
- PrepareOptions{MaxChunkRunes int}
- PreparedOutput{Translated string, Chunks []Chunk}
- Chunk{Index int, Total int, Content string}

Implementation requirements:
1. Find the target project's Feishu sender or message delivery path.
2. Replace any ad hoc markdown translation/chunking logic with a call to PrepareCodexMarkdown for Codex markdown inputs.
3. Preserve existing plain-text behavior for non-markdown or non-Codex paths.
4. Send chunks in order using Chunk.Content.
5. If the Feishu interactive send path can fail for a chunk, retry that same chunk as plain text before giving up, unless the target project already has a stricter policy that must be preserved.
6. Keep the caller's existing thread / reply / receipt behavior unchanged.
7. Do not reimplement markdown translation rules unless absolutely necessary; use the library.

Recommended wiring:
- Use PrepareOptions{MaxChunkRunes: feishumarkdown.DefaultMaxChunkRunes} as the default.
- If the target project already has a separate plain-text chunk budget, keep a sender-side cap for interactive markdown and pass the smaller value into PrepareCodexMarkdown.
- If the project needs only translation and not chunking, use TranslateCodexMarkdownToFeishu directly.
- If the project already translated the markdown earlier, use SplitTranslatedMarkdown on that translated output instead of translating twice.

Important caveat:
- The library appends chunk suffix markers like [1/3] after splitting. If the target project has a strict final-message length budget, reserve a little extra room before calling PrepareCodexMarkdown.

Acceptance criteria:
- Existing tests for message delivery still pass.
- New tests cover the library integration path and prove that chunks are sent in order.
- Long markdown inputs split cleanly, and fenced code blocks / tables remain readable.
- The project still falls back to plain text on interactive markdown failure if that is part of its current behavior.

Verification:
- Run the relevant package tests.
- Run the full Go test suite if the project is small enough.
- Manually inspect at least one long markdown example to confirm chunk suffixes and rendering are acceptable.
```

