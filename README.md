# frieren-clone

Feishu bot for Codex-style replies, plus a reusable markdown preparation library.

## Quickstart

1. If you want to integrate the reusable library, start with [`docs/specs/2026-03-20-feishu-markdown-library-usage.md`](docs/specs/2026-03-20-feishu-markdown-library-usage.md).
2. If you want a copy-paste handoff prompt for another coding agent, use [`docs/specs/2026-03-20-feishu-markdown-library-agent-prompt.md`](docs/specs/2026-03-20-feishu-markdown-library-agent-prompt.md).
3. In Go code, call `feishumarkdown.PrepareCodexMarkdown(input, feishumarkdown.PrepareOptions{})` for raw Codex markdown and send the returned chunks in order.

The library default chunk budget is `1380` runes. If you use a separate interactive markdown sender budget, keep the sender cap in sync so final Feishu cards stay within the expected size range.
