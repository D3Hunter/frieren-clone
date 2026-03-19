# CommonMark/GFM to Feishu Translator Plan (Codex Output)

## Summary
Build an AST-based translator that converts Codex CommonMark/GFM output into Feishu interactive card Markdown with maximum fidelity, then send it via JSON 2.0 `markdown` elements (instead of current `div.text.lark_md`).
This plan applies only to Codex-generated responses and keeps help/heartbeat/system messages unchanged.

## Chosen Approach
1. We will use an AST pipeline (Goldmark + GFM extensions) and render normalized Feishu-friendly Markdown, not regex-only conversion.
2. We will use Feishu interactive JSON 2.0 `markdown` content for Codex output because it has the best coverage for headings, lists, quotes, code blocks, and tables.
3. We will keep a safety fallback: if card creation fails for a translated chunk, retry that chunk once as plain `text`.

## Key Changes
1. Add a render mode contract between service and sender.
   - Extend `OutgoingMessage` in `pkg/service/message_service.go` with a render mode field (default plain; Codex markdown mode for Codex responses).
   - Extend sender request mapping in `cmd/frieren/main.go` and `pkg/sender/text_sender.go` to carry this mode through.

2. Implement translator module.
   - Create `pkg/sender/markdown_translator.go` with an AST-based translator:
   - Parse CommonMark/GFM.
   - Preserve standard markdown blocks/inline formatting.
   - Convert non-http/non-https links (for example local file paths) to readable inline code/path text.
   - Escape raw HTML by default.
   - Convert GFM task list markers into readable checkbox bullets.
   - Normalize nested list indentation to Feishu-friendly format.
   - Output a canonical markdown string for Feishu card rendering.

3. Move Codex interactive payload to JSON 2.0 markdown element.
   - In `pkg/sender/text_sender.go`, build interactive content as schema 2.0 card body with `tag: "markdown"` and translated content.
   - Keep non-Codex send paths unchanged unless explicitly marked for Codex markdown rendering.

4. Add markdown-aware chunking.
   - Replace rune-only chunking for Codex markdown mode with block-aware chunk splitting.
   - Never split inside fenced code blocks, table blocks, or list item blocks when avoidable.
   - Preserve `[i/n]` ordering prefix per chunk while keeping valid markdown structure.
   - Keep existing chunk size guardrails and request-size safety.

5. Apply Codex-only routing in service.
   - In `pkg/service/message_service.go`, set Codex render mode for:
   - `/<project> <prompt>`
   - `/mcp call codex ...`
   - topic follow-up replies (`codex-reply`)
   - Keep `/help`, `/mcp tools`, `/mcp schema`, heartbeat, and failure/system notices plain text mode.

6. Update canonical behavior spec.
   - Revise sender/formatting section in `docs/specs/2026-03-17-feishu-mcp-command-format.md` to document:
   - Codex output translation pipeline
   - JSON 2.0 markdown card payload
   - fallback-to-text retry behavior
   - codex-only scope for rich translation

## Test Plan
1. Translator unit tests:
   - Preserve headings, emphasis, blockquotes, fenced code, ordered/unordered lists, and tables.
   - Convert local file links to readable code/path text.
   - Escape raw HTML blocks/inline HTML.
   - Convert task lists to readable degraded form.

2. Sender unit tests:
   - Codex markdown mode sends interactive JSON 2.0 `markdown` payload.
   - Non-Codex mode behavior remains unchanged.
   - Card creation error triggers one plain-text retry path.

3. Chunking unit tests:
   - Long markdown splits into ordered chunks without breaking fenced code/table structures.
   - Reconstructed content from chunks remains semantically equivalent.

4. Service tests:
   - Codex command/follow-up paths set markdown render mode.
   - Non-Codex command paths remain plain text mode.

5. End-to-end verification:
   - Run `go test ./...`.
   - Manual scenario with real Codex-like output containing table + code block + local path links; verify Feishu rendering and fallback behavior.

## Assumptions and Defaults
- Confirmed choices from this planning session:
- AST-based translator.
- Codex responses always use rich interactive path.
- Markdown-aware chunking.
- Unsupported features degrade to readable output.
- Local file links become readable code/path text, not hyperlinks.
- Raw HTML is escaped by default.
- Rich translation scope is Codex outputs only.
- For best Codex fidelity, implementation targets Feishu interactive JSON 2.0 `markdown` elements.

- Dependency default:
- Add `github.com/yuin/goldmark` with GFM extensions for parsing/rendering.

- Capability matrix is based on Feishu official docs for:
- Send message API (`interactive` support),
- message/post `md` markdown subset,
- card `lark_md` legacy subset,
- card JSON 2.0 markdown component (CommonMark-oriented support).
