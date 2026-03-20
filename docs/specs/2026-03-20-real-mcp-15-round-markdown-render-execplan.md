# Real-MCP 15-Round Markdown Rendering Hardening

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

`PLANS.md` is checked in at the repository root and this document is maintained in accordance with `/Users/jujiajia/code/frieren-clone/PLANS.md`.

## Purpose / Big Picture

A simulation loop that only checks leaked JSON fragments is not enough to catch Feishu markdown compatibility issues such as heading levels that the client does not render visibly. After this change, running `bin/frieren` in simulation mode against the real MCP server for 15 rounds will fail if known non-renderable heading levels leak into outgoing markdown, and the markdown translator will normalize those headings into Feishu-compatible levels.

## Progress

- [x] (2026-03-20 10:49 CST) Re-read canonical command/format spec and current simulator/translator behavior.
- [x] (2026-03-20 10:49 CST) Verified real MCP endpoint availability at `http://localhost:8787/mcp` and located usable config (`prod.toml`) with existing `tidb` cwd.
- [x] (2026-03-20 10:54 CST) Added failing tests for heading-compatibility detection and translation normalization (`pkg/sender/markdown_translator_test.go`, `cmd/frieren/simulation_test.go`) and confirmed red state.
- [x] (2026-03-20 10:54 CST) Implemented translator + simulator detection changes to satisfy failing tests (H5/H6 normalization + heading-risk detection).
- [x] (2026-03-20 11:03 CST) During first real-MCP run, found and fixed detector false-positive on inline triple-backtick substrings by switching fence detection from raw substring counting to fence-delimiter-line counting, with a dedicated failing test.
- [ ] Execute requested loop: clean logs, build binary, run real-MCP simulation for 15 rounds.
- [ ] If simulation reports unrendered output, iterate fixes and rerun until clean.
- [ ] Run final `go test ./...` verification and record outcomes.

## Surprises & Discoveries

- Observation: The real MCP endpoint is reachable and responds immediately, so the requested real-MCP simulation loop is feasible in this environment.
  Evidence: `curl http://localhost:8787/mcp` returns HTTP 400 with body `Invalid or missing session ID`, proving service reachability.

- Observation: The first real-MCP simulation attempt raised an unrendered detection that traced to detector logic, not to leaked JSON structure.
  Evidence: `simulation sender` logged `unrendered markdown artifact detected` while preview text showed plain markdown prose; root cause was `strings.Count(text, \"```\")%2 != 0`, which over-counts inline triple-backtick substrings.

## Decision Log

- Decision: Use `prod.toml` for real-MCP simulation runs.
  Rationale: `example.toml` points `projects.tidb.cwd` to `/Users/jujiajia/code/tidb`, which does not exist in this workspace; `prod.toml` points to `/Users/jujiajia/code/pingcap/tidb`, which exists.
  Date/Author: 2026-03-20 / Codex

- Decision: Keep fence-balance detection but scope it to real fenced-code delimiter lines only.
  Rationale: This preserves useful detection for unmatched code fences while avoiding false positives from inline triple-backtick substrings in regular text.
  Date/Author: 2026-03-20 / Codex

## Outcomes & Retrospective

Pending implementation.

## Context and Orientation

The simulation entry point is in `/Users/jujiajia/code/frieren-clone/cmd/frieren/simulation.go`. In simulation mode, the bot injects a mocked inbound message, runs normal command-service flow, and sends replies through mocked sender/reaction APIs. The mock sender currently extracts markdown content from interactive payloads and counts failures via `failureCount` and `unrenderedCount`.

Markdown conversion is implemented in `/Users/jujiajia/code/frieren-clone/pkg/sender/markdown_translator.go`. The translator currently downgrades only H1 to H2, based on Feishu compatibility observations. It does not normalize H5/H6 levels.

The current unrendered detector (`hasUnrenderedArtifacts`) checks only raw structured payload leak markers and malformed code fences. It does not treat known unsupported heading levels as rendering risk.

## Plan of Work

First, create failing tests (TDD red phase) in translator and simulation tests. Translator tests will assert that H5/H6 headings are downgraded to a compatible level. Simulation tests will assert that markdown containing H5/H6 heading markers is treated as unrendered risk, while H4 remains valid.

Second, implement behavior updates in `pkg/sender/markdown_translator.go` and `cmd/frieren/simulation.go` to satisfy those tests. Translator will clamp headings into a compatibility band (H2-H4), preserving current H1-to-H2 behavior and additionally reducing H5/H6 to H4. Simulator artifact detection will include heading-level compatibility checks so the 15-round loop can fail on this class of issue.

Third, execute the runtime loop exactly as requested with real MCP enabled: truncate log files in `logs/`, run `make build`, run `FRIEREN_SIMULATION_MODE=1 FRIEREN_SIMULATION_REAL_MCP=1 FRIEREN_SIMULATION_ROUNDS=15 ./bin/frieren -config prod.toml`, then inspect logs for unrendered detections. If any are found, iterate code+tests and rerun until clean. During inspection, prioritize true structured-leak/compatibility artifacts and avoid detector regressions by keeping failing tests for detector edge cases.

## Concrete Steps

From `/Users/jujiajia/code/frieren-clone`:

1. Run targeted tests (expect failure before code changes):
   `go test ./pkg/sender ./cmd/frieren`
2. Implement code changes and rerun targeted tests (expect pass).
3. Clean logs by truncation:
   `: > logs/frieren.log`
4. Build:
   `make build`
5. Run 15-round simulation against real MCP:
   `FRIEREN_SIMULATION_MODE=1 FRIEREN_SIMULATION_REAL_MCP=1 FRIEREN_SIMULATION_ROUNDS=15 ./bin/frieren -config prod.toml`
6. Inspect logs:
   `rg -n "unrendered|simulation mode finished|failure_count|Execution failed" logs/frieren.log`
7. Full verification:
   `go test ./...`

## Validation and Acceptance

Acceptance criteria:

1. Translator tests prove H5/H6 headings are normalized into Feishu-compatible heading levels.
2. Simulation tests prove unrendered detection flags unsupported heading levels.
3. Running the requested real-MCP 15-round simulation finishes successfully with `failure_count=0` and `unrendered_count=0`.
4. `go test ./...` passes after all changes.

## Idempotence and Recovery

The workflow is rerunnable. Log cleanup uses truncation, which is safe to repeat. Simulation mode is isolated from live Feishu long-connection handling, so repeated runs do not require manual state reset. If a run fails, inspect logs, patch translator/detection logic, and rerun the same command sequence.

## Artifacts and Notes

Important artifacts to capture during execution:

- Failing-to-passing test evidence for translator and simulator heading compatibility.
- `logs/frieren.log` summary line with simulation totals for the 15-round real-MCP run.

## Interfaces and Dependencies

No new dependencies are planned. Existing interfaces remain unchanged:

- `translateCodexMarkdownToFeishu(input string) (string, error)` in `pkg/sender/markdown_translator.go`
- `hasUnrenderedArtifacts(text string) bool` in `cmd/frieren/simulation.go`
- Simulation mode flags in `cmd/frieren/simulation.go`:
  - `FRIEREN_SIMULATION_MODE`
  - `FRIEREN_SIMULATION_REAL_MCP`
  - `FRIEREN_SIMULATION_ROUNDS`

---

Revision note (2026-03-20, Codex): Created plan for real-MCP 15-round rendering-hardening loop focused on making simulator checks meaningful for heading compatibility, not only structured-payload leakage.
Revision note (2026-03-20, Codex): Updated plan after first runtime attempt uncovered a detector false-positive; added test-driven fix and decision rationale.
