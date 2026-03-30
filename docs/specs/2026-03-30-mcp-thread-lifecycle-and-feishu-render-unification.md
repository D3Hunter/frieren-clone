# MCP Thread Lifecycle and Feishu Render Unification

Date: 2026-03-30  
Status: Implemented

## 1) Why this change

Two runtime behaviors needed to be aligned with current Codex MCP usage:

- Frieren should not explicitly close/delete MCP thread/session state because Codex thread state is retained in MCP memory unless closed.
- Feishu outbound rendering should use one unified markdown conversion path instead of splitting between plain text and codex markdown paths.

## 2) Behavior changes

### A. MCP session lifecycle in Frieren

Before:

- Gateway rotated/closed cached MCP session after idle timeout.
- Main runtime and simulation runtime explicitly closed gateway on process shutdown paths.

After:

- Gateway keeps one long-lived cached session and does not auto-close by idle timeout.
- Main runtime and simulation runtime no longer issue explicit gateway close calls in normal shutdown flow.
- Gateway still handles transport-closed errors by dropping local cached session and reconnecting once when needed.

### B. Feishu render path

Before:

- Service sent some messages in `plain_text` render mode (`/help`, `/mcp tools`, `/mcp schema`, heartbeats, failure notices, reset notices).
- Codex result flows used `codex_markdown` render mode.

After:

- Service/sender runtime normalizes all outbound messages to markdown-convert interactive rendering.
- Sender keeps existing per-chunk fallback: if interactive send fails, retry same chunk as plain `text`.

### C. Command guidance copy

Before:

- Help and usage strings were terse (`Available commands`, `Usage: ...`, `Unknown project alias: ...`).
- Some fallback paths used short diagnostic-style wording without examples.

After:

- `/help` returns a human-friendly command guide with descriptions and quick tips.
- `/help` also lists currently configured project aliases so users can discover valid `/<project> <prompt>` prefixes directly.
- Usage and fallback messages include clearer action wording and example command formats.
- Unknown project alias and unbound-topic guidance now point users to `/help` and provide next-step context.

## 3) Code updates

- `pkg/mcp/adapter.go`
  - Removed idle-timeout-driven session close path.
  - Kept reconnect-on-closed-transport recovery path.
- `cmd/frieren/main.go`
  - Removed deferred explicit `mcpGateway.Close()` call.
- `cmd/frieren/simulation.go`
  - Removed deferred explicit close logic for real MCP gateway mode.
- `pkg/service/message_service.go`
  - Updated session-reset notice wording to avoid "automatically closed after 1h" claim.
  - Normalized runtime render mode selection to unified markdown mode.
  - Refactored command/help/usage/fallback copy to be more human-readable and action-oriented.
- `pkg/sender/text_sender.go`
  - Normalized runtime render mode selection to unified markdown mode while preserving interactive->text fallback.

## 4) Test updates

- `pkg/mcp/adapter_test.go`
  - Replaced idle-timeout recreation expectation with "keep session after idle period" expectation.
- `pkg/service/command_service_test.go`
  - Updated render mode expectations for help and session-reset notice to markdown mode.
  - Updated session-reset notice text assertion.
- `pkg/sender/text_sender_test.go`
  - Updated default-mode tests to expect interactive markdown send path.
  - Updated long-output chunk test to assert interactive markdown chunk payloads.

## 5) Verification

Executed from repository root:

- `go test ./...`

Result:

- All packages pass.

## 6) Canonical spec alignment

Canonical source of truth was updated in:

- `docs/specs/2026-03-17-feishu-mcp-command-format.md`

Key aligned sections:

- MCP session policy (no idle auto-close and no explicit normal-runtime close behavior).
- Session-not-found fallback wording.
- Sender content mode and chunking behavior under unified markdown conversion.
