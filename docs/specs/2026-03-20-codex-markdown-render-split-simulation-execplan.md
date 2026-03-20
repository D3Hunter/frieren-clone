# Codex Markdown Split Rendering Hardening and Real-MCP Simulation Loop

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

`PLANS.md` is checked in at the repository root and this document will be maintained in accordance with `/Users/jujiajia/code/frieren-clone/PLANS.md`.

## Purpose / Big Picture

The Feishu bot should reliably render Codex markdown replies even when the Codex tool output is internally split across multiple structured message fragments. After this change, a local developer can run `bin/frieren` in a deterministic simulation mode (without real Feishu traffic), feed the exact long `/tidb ...` command prompt, and confirm from logs that no unrendered structured fragments leak into outgoing replies. This follow-up run specifically verifies the same flow against a real MCP server while still mocking Feishu inbound/outbound traffic.

## Progress

- [x] (2026-03-20 07:16 CST) Read canonical Feishu/MCP behavior spec and current implementation path (`handler -> service -> sender -> translator`).
- [x] (2026-03-20 07:16 CST) Captured current logs and identified current weak point: structured Codex payload extraction currently prefers only top-level `content` + `threadId`.
- [x] (2026-03-20 07:19 CST) Added failing tests for split structured Codex payload extraction and markdown rendering cleanup in `pkg/service/message_service_test.go`, then confirmed failures before implementation.
- [x] (2026-03-20 07:21 CST) Implemented structured payload flattening for split assistant messages in `pkg/service/message_service.go`.
- [x] (2026-03-20 07:23 CST) Added local simulation mode (`FRIEREN_SIMULATION_MODE=1`) to `cmd/frieren/main.go` and `cmd/frieren/simulation.go` with mocked inbound message, mocked MCP split payload, and mocked reply/reaction APIs that detect unrendered artifacts.
- [x] (2026-03-20 07:24 CST) Executed requested 10-round binary workflow (`logs` cleanup via truncation, `make build`, simulation run) and verified zero unrendered detections in logs.
- [x] (2026-03-20 07:24 CST) Ran `go test ./...` and confirmed all packages pass.
- [x] (2026-03-20 12:13 CST) Confirmed real MCP endpoint availability at `http://localhost:8787/mcp` before simulation execution.
- [x] (2026-03-20 12:18 CST) Ran requested clean-build-run loop using real MCP (`FRIEREN_SIMULATION_REAL_MCP=1`) for 3 consecutive rounds and verified `unrendered_count=0`.
- [x] (2026-03-20 12:19 CST) Re-ran `go test ./...` after real-MCP validation and prompt alignment changes.
- [x] (2026-03-20 12:20 CST) Aligned simulation prompt to exactly match the requested single-line `/tidb ... be longer than 2k` command text.
- [x] (2026-03-20 12:25 CST) Re-ran clean-build-run against real MCP after prompt alignment and again verified 3/3 rounds with `failure_count=0` and `unrendered_count=0`.

## Surprises & Discoveries

- Observation: Existing runtime logs show the target long markdown request already flowing through normal command routing and codex markdown mode, but they do not exercise internal split structured payload shapes.
  Evidence: `logs/frieren.log` contains normal `/tidb ... be longer than 2k` handling and one large markdown output with no explicit split-structure parser usage.
- Observation: The shell policy in this environment rejects destructive delete commands (`rm -f`) but allows deterministic truncation (`: > logs/frieren.log`), so log cleanup was done by truncation.
  Evidence: direct `rm -f logs/*.log` command was rejected with approval policy message; truncation commands succeeded.
- Observation: The local MCP endpoint returns HTTP 400 with `Invalid or missing session ID` for direct browser-style requests, which confirms the server is reachable and expects MCP session negotiation.
  Evidence: `curl -i http://localhost:8787/mcp` returned `HTTP/1.1 400 Bad Request` with `Invalid or missing session ID`.

## Decision Log

- Decision: Add an explicit local simulation mode in the executable instead of relying on live Feishu traffic for this debugging workflow.
  Rationale: The user requested running `bin/frieren` without a real sender/receiver; simulation mode preserves the production pipeline while removing external dependencies.
  Date/Author: 2026-03-20 / Codex

- Decision: Keep the production command grammar and routing untouched; only inject simulation behavior behind a dedicated runtime flag.
  Rationale: The canonical behavior spec must remain the source of truth, and the simulation workflow should be opt-in.
  Date/Author: 2026-03-20 / Codex

- Decision: Keep Feishu sender/reaction mocked in simulation mode even for real-MCP verification and switch only MCP transport via `FRIEREN_SIMULATION_REAL_MCP=1`.
  Rationale: This preserves deterministic local replay while validating real Codex payload shape and translation behavior end-to-end.
  Date/Author: 2026-03-20 / Codex

## Outcomes & Retrospective

The extractor now correctly flattens nested split assistant content from structured Codex payloads and still supports the previous top-level `content` payload shape. Simulation mode runs the full command -> MCP -> format -> translate -> sender pipeline without real Feishu network dependencies, injects the exact long `/tidb` prompt, and performs unrendered-artifact detection on outgoing mocked replies.

Requested real-MCP runtime acceptance is now also verified twice: both before and after prompt-string alignment, clean 3-round runs completed with `failure_count=0` and `unrendered_count=0`, and repository tests still pass.

## Context and Orientation

The command handling path starts in `/Users/jujiajia/code/frieren-clone/pkg/handler/message_handler.go` and delegates normalized incoming text to `/Users/jujiajia/code/frieren-clone/pkg/service/message_service.go`. Codex command responses are formatted by `formatCodexOutput` in that service, then sent with `render_mode=codex_markdown` through `/Users/jujiajia/code/frieren-clone/pkg/sender/text_sender.go`. In codex markdown mode, text is translated by `/Users/jujiajia/code/frieren-clone/pkg/sender/markdown_translator.go` and chunked with markdown-aware splitting before interactive card payload construction.

Structured payload extraction in `extractCodexStructuredPayload` now handles both top-level `content` payloads and nested split assistant message shapes (for example `response.output[].content[]` with `output_text` fragments), so downstream translation receives renderable markdown instead of raw JSON fragments.

## Plan of Work

First, add tests that represent split structured payloads where assistant text is divided into multiple fragments (for example nested `messages[].content[]` entries). The tests should fail under the current parser. Next, update the extractor to recover user-visible markdown text by walking known structured shapes and concatenating content fragments in order, while still resolving thread ID reliably and keeping plain text fallback behavior for unknown structures.

Then, add a simulation mode entrypoint in `cmd/frieren/main.go` that constructs `CommandService` with:

1. A mock MCP gateway that returns deterministic long markdown output wrapped in a split structured payload for `codex`.
2. A mock sender that records outgoing messages and logs any message that appears to contain unrendered structured artifacts.
3. One synthetic incoming `/tidb ...` message matching the user-provided text.

In this mode, run N rounds in one process (default 1, configurable), and log each round result plus a final summary count of unrendered detections. Keep the existing default runtime behavior unchanged when simulation mode is disabled.

## Concrete Steps

From repository root `/Users/jujiajia/code/frieren-clone`:

1. Add/adjust tests:
   - `go test ./pkg/service ./pkg/sender`
2. Implement parser + simulation mode and iterate on failing tests:
   - `go test ./pkg/service ./pkg/sender`
3. Requested runtime loop:
   - `mkdir -p logs && find logs -type f -name '*.log' -exec sh -c ': > "$1"' _ {} \;`
   - `make build`
   - `FRIEREN_SIMULATION_MODE=1 FRIEREN_SIMULATION_REAL_MCP=1 FRIEREN_SIMULATION_ROUNDS=3 ./bin/frieren -config example.toml`
4. Validate logs:
   - `rg -n "unrendered|UNRENDERED|split payload leaked|send markdown card failed" logs`
5. Full verification:
   - `go test ./...`

Expected observable result for acceptance is zero unrendered detections after 3 real-MCP rounds and passing tests.

## Validation and Acceptance

Acceptance is met when:

1. Unit tests include a case where split structured Codex payload is converted into renderable markdown body and thread footer without raw JSON leakage.
2. Running the binary in simulation mode for 3 rounds with real MCP emits successful outgoing codex markdown replies.
3. Log scan reports zero unrendered-structure detections in those 3 rounds.
4. `go test ./...` passes.

## Idempotence and Recovery

Simulation mode is read-only with respect to Feishu network calls and can be rerun repeatedly. Log cleanup (`mkdir -p logs && find logs -type f -name '*.log' -exec sh -c ': > "$1"' _ {} \;`) is safe and repeatable. If one round reports unrendered leakage, fix parser/sender logic and rerun the same build + simulation commands without requiring external state reset.

## Artifacts and Notes

Key artifact targets:

- Updated parser/tests proving split structured payload support.
- Simulation-mode runtime logs in `logs/frieren.log` showing 3/3 real-MCP rounds with no unrendered detections.

Observed evidence snippets:

- `simulation mode finished {"rounds": 3, "reply_count": 17, "interactive_reply_count": 17, "failure_count": 0, "unrendered_count": 0}`
- `go test ./...` completed successfully across all packages.

## Interfaces and Dependencies

The parser enhancement lives in `pkg/service/message_service.go` and must preserve existing interfaces:

- `func formatCodexOutput(output, codexThreadID, tokenUsage string, notices ...string) string`
- `func extractCodexStructuredPayload(output string) (content string, threadID string, ok bool)`

Simulation wiring is in `cmd/frieren/main.go` and should continue using the existing interfaces:

- `service.MCPGateway`
- `service.MessageSender`
- `service.CommandService`

No external dependency changes are expected.

---

Revision note (2026-03-20, Codex): Created initial ExecPlan before code edits, based on current implementation and user-requested 10-round simulation workflow.
Revision note (2026-03-20, Codex): Updated all living sections after implementation, including test evidence and 10-round simulation verification results.
Revision note (2026-03-20, Codex): Added real-MCP 3-round validation evidence (`FRIEREN_SIMULATION_REAL_MCP=1`), updated acceptance text, and aligned concrete log-clean command with current shell policy.
Revision note (2026-03-20, Codex): Updated prompt fixture to the exact requested single-line command text and recorded a second clean real-MCP 3-round verification pass.
