# Feishu Codex Markdown Library Extraction and Reuse Enablement

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

`PLANS.md` is checked in at the repository root and this document must be maintained in accordance with `/Users/jujiajia/code/frieren-clone/PLANS.md`.

## Purpose / Big Picture

After this change, the Codex-markdown-to-Feishu translation and safe chunking logic will be available as a reusable Go library that other projects and agents can consume with one function call. A consumer will pass raw Codex markdown and receive Feishu-safe ordered chunks, ready to send as interactive markdown content. Existing Frieren runtime behavior must remain unchanged so current Feishu output quality and topic-thread behavior stay stable.

The immediate user-visible result is not a new bot command. The result is a reusable package and complete usage documentation, including a single copy-paste, model-agnostic integration prompt that another coding agent can run in one shot inside a target Go project.

## Progress

- [x] (2026-03-20 12:55 CST) Inspected current implementation and verified that Codex markdown translation and markdown-aware chunking are split across `pkg/sender/markdown_translator.go` and `pkg/sender/text_sender.go`.
- [x] (2026-03-20 12:56 CST) Confirmed extraction scope and guardrails: in-repo package, one-call API, Codex-markdown scope only, no intentional behavior changes, and subtask diff budget under 1000 LOC each.
- [x] (2026-03-20 12:58 CST) Authored this ExecPlan under `docs/specs` per repository rules.
- [x] (2026-03-20 16:35 CST) Milestone 1 complete: created `pkg/feishumarkdown` package shell (`doc.go`, `prepare.go`) with exported API contract and minimal tests in `prepare_test.go`; verified with `go test ./pkg/feishumarkdown ./pkg/sender`; milestone diff size: 93 insertions.
- [x] (2026-03-20 16:42 CST) Milestone 2 complete: moved translator runtime into `pkg/feishumarkdown/translator.go`, wired `PrepareCodexMarkdown` through the extracted translator, kept `pkg/sender/markdown_translator.go` as a thin compatibility wrapper, and added package-level translator parity tests; verified with a red-green cycle using `go test ./pkg/feishumarkdown` before extraction and `go test ./pkg/feishumarkdown ./pkg/sender` after extraction.
- [x] (2026-03-20 17:30 CST) Milestone 3 complete: moved markdown-aware splitter runtime into `pkg/feishumarkdown/splitter.go`, made `PrepareCodexMarkdown` compose translation + splitting + markdown suffix ordering markers, kept sender behavior unchanged via a thin splitter wrapper, and moved milestone-critical splitter parity coverage into package-level tests; verified with a red-green cycle using `go test ./pkg/feishumarkdown` (initially failed with `undefined: splitMarkdownChunks`) and `go test ./pkg/feishumarkdown ./pkg/sender` after extraction.
- [x] (2026-03-20 17:36 CST) Milestone 4 complete: refactored `pkg/sender/text_sender.go` to call `feishumarkdown.PrepareCodexMarkdown` for codex markdown mode, kept plain-text chunking untouched, preserved per-chunk interactive-then-text fallback semantics, removed sender-local markdown translation/splitting flow from runtime, and verified with a red-green cycle using `go test ./pkg/sender` (initially failed with `undefined: prepareCodexMarkdown`) followed by `go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service`; milestone diff size: `git diff --shortstat` => `3 files changed, 196 insertions(+), 29 deletions(-)`.
- [x] (2026-03-20 18:24 CST) Milestone 5 complete: moved remaining translator-compatibility assertions and default interactive-safety chunk coverage into `pkg/feishumarkdown`, trimmed sender tests back to delivery responsibilities, removed `pkg/sender/markdown_translator.go` plus its redundant parser-focused test file, and verified with `go test ./pkg/feishumarkdown`, `go test ./pkg/sender`, and `go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service`; milestone diff size: `git diff --stat -- pkg/feishumarkdown pkg/sender` => `5 files changed, 206 insertions(+), 755 deletions(-)`.
- [x] (2026-03-20 17:58 CST) Milestone 6 complete: created `docs/specs/2026-03-20-feishu-markdown-library-usage.md` and `docs/specs/2026-03-20-feishu-markdown-library-agent-prompt.md`, and updated `README.md` with a concise quickstart plus links to both docs.
- [x] (2026-03-20 18:00 CST) Milestone 7 complete: ran the targeted package suite and the full repository suite, then recorded the pass results in this plan.

## Surprises & Discoveries

- Observation: The extraction touches large files even without behavior changes.
  Evidence: `wc -l` shows `pkg/sender/markdown_translator.go` (568), `pkg/sender/markdown_translator_test.go` (450), `pkg/sender/text_sender.go` (712), and `pkg/sender/text_sender_test.go` (710).

- Observation: Markdown chunking logic is currently embedded in sender internals, not isolated behind a package boundary.
  Evidence: `splitMarkdownChunks`, `splitMarkdownBlocks`, and fence parsing helpers live in `pkg/sender/text_sender.go`.

- Observation: Plain-text chunking is still required for non-Codex flows and should not be moved in this change.
  Evidence: Sender uses plain mode for help, schema/tools replies, heartbeat, and failure/system text, with `splitChunks` in `pkg/sender/text_sender.go`.

- Observation: Milestone 1 default option normalization was initially non-observable through public output shape and required an internal seam for meaningful tests.
  Evidence: `normalizePrepareOptions` was introduced and covered by `TestNormalizePrepareOptions_*` to ensure `MaxChunkRunes <= 0` normalizes to `DefaultMaxChunkRunes` while preserving explicit values.

- Observation: Sender translator tests still reach into an unexported helper, so removing `pkg/sender/markdown_translator.go` entirely would have forced premature sender test refactoring.
  Evidence: `pkg/sender/markdown_translator_test.go` contains `TestRenderInlineCode_UsesFenceLongerThanContainedBackticks`, which directly exercises `renderInlineCode`.

- Observation: `git diff --shortstat` does not include the new untracked package files created during this milestone, so it understates the working-tree size until those files are staged or otherwise accounted for.
  Evidence: After creating `pkg/feishumarkdown/translator.go` and `pkg/feishumarkdown/translator_test.go`, `git diff --shortstat` reported only tracked-file edits while `git status --short` still showed both package files as untracked.

- Observation: Milestone 3 could move the splitter without switching sender to the full library pipeline by leaving a thin sender-local wrapper around the extracted splitter entry point.
  Evidence: `pkg/sender/text_sender.go` now delegates `splitMarkdownChunks` to `feishumarkdown.SplitTranslatedMarkdown`, while sender still performs translation and per-chunk send/fallback exactly as before.

- Observation: Sender still needs to supply an explicit markdown chunk cap when calling the library because the sender's general chunk budget (`1800`) is intentionally higher than the interactive-safe markdown budget (`1380`).
  Evidence: `TestSend_CodexMarkdownModeUsesPreparedChunksFromLibrary` asserts that sender passes `defaultMaxMarkdownChunkRunes` into `PrepareCodexMarkdown`, and the targeted verification set stays green.

- Observation: A tiny package-level function seam in sender makes the new library integration directly unit-testable without changing runtime behavior.
  Evidence: Milestone 4 red state was `go test ./pkg/sender` failing with `undefined: prepareCodexMarkdown`; after introducing the seam and wiring `Send` through it, the new sender tests passed.

- Observation: Prepared markdown chunks can grow slightly beyond a caller-supplied split budget because `[i/n]` ordering suffixes are appended after splitting.
  Evidence: A targeted check against `PrepareCodexMarkdown(..., PrepareOptions{MaxChunkRunes: 1400})` produced chunks of `1402` runes, while the default `1380` budget stayed under the sender's historical `1400` interactive-safety ceiling.

- Observation: Final verification completed cleanly with no new failures or behavioral deviations.
  Evidence: `go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service` and `go test ./...` both passed in the final milestone run.

## Decision Log

- Decision: Keep extraction inside the current module (`github.com/D3Hunter/frieren-clone`) for v1.
  Rationale: Fastest adoption path with least release overhead while still enabling reuse through importable package paths.
  Date/Author: 2026-03-20 / Codex

- Decision: Expose one high-level pipeline API as the primary consumer surface.
  Rationale: Other projects/agents should be able to integrate with one call and avoid stitching translator/splitter details.
  Date/Author: 2026-03-20 / Codex

- Decision: Scope this effort to Codex-markdown mode only.
  Rationale: Matches request and avoids risky scope growth into plain-text chunking behavior.
  Date/Author: 2026-03-20 / Codex

- Decision: Treat extraction as refactor-only with no intentional behavior changes.
  Rationale: Existing Feishu rendering behavior is already tuned; preserving compatibility is safer than opportunistic changes during boundary moves.
  Date/Author: 2026-03-20 / Codex

- Decision: Enforce implementation batches under 1000 changed LOC each.
  Rationale: Smaller diffs reduce review risk and allow clean rollback if parity regressions appear.
  Date/Author: 2026-03-20 / Codex

- Decision: Keep Milestone 1 behavior intentionally minimal while adding an unexported option-normalization helper for test signal quality.
  Rationale: The milestone contract does not expose chunk-budget effects yet, so helper-level tests validate default handling without introducing premature runtime behavior.
  Date/Author: 2026-03-20 / Codex

- Decision: Make `PrepareCodexMarkdown` call the extracted translator during Milestone 2 even though chunk assembly remains a later milestone.
  Rationale: This keeps the public package entry point observably useful now, lets TDD assert the extraction through package API behavior, and avoids introducing a second temporary translation seam.
  Date/Author: 2026-03-20 / Codex

- Decision: Retain a thin sender wrapper plus the local `renderInlineCode` helper until sender-local parser tests are rebalanced in Milestone 5.
  Rationale: This keeps sender-facing behavior and existing sender tests stable without pulling test-migration work into the translator-extraction milestone.
  Date/Author: 2026-03-20 / Codex

- Decision: Keep the interactive-safe markdown cap logic in sender even after switching runtime preparation to `feishumarkdown.PrepareCodexMarkdown`.
  Rationale: The library default matches the safe Feishu markdown budget, but sender still carries a broader plain-text chunk budget (`1800`); preserving the sender-side cap avoids accidental single-card sends if that plain-text budget changes or is overridden in tests.
  Date/Author: 2026-03-20 / Codex

- Decision: Introduce a sender-local `prepareCodexMarkdown` function variable as a test seam.
  Rationale: This allows sender tests to prove that `Send` consumes prepared library chunks directly and preserves per-chunk fallback behavior, without adding a new exported interface or changing runtime call sites.
  Date/Author: 2026-03-20 / Codex

- Decision: Remove the sender-local translator compatibility wrapper and migrate the remaining compatibility assertions into `pkg/feishumarkdown` during Milestone 5.
  Rationale: Sender no longer owns markdown translation behavior at runtime, so keeping parser-focused tests or wrapper-only helpers under `pkg/sender` would preserve duplication without adding sender-specific confidence.
  Date/Author: 2026-03-20 / Codex

## Outcomes & Retrospective

Milestone 1 shipped the reusable package shell and one-call API contract in `pkg/feishumarkdown` without changing runtime sender behavior. Milestone 2 moved the markdown translation runtime into that package, added package-level translator parity coverage, and kept sender behavior stable via a thin wrapper. Milestone 3 moved markdown-aware splitting into `pkg/feishumarkdown`, made `PrepareCodexMarkdown` return sender-compatible ordered chunks, and kept sender runtime behavior stable by delegating only the splitter helper. Milestone 4 finished the first real consumer migration by switching `pkg/sender/text_sender.go` to the library pipeline, leaving plain-text handling untouched and preserving the per-chunk interactive fallback path. Milestone 5 rebalanced the tests around that boundary: compatibility rules now primarily live under `pkg/feishumarkdown`, sender tests focus on delivery semantics, and the obsolete sender-local translator wrapper has been removed. Milestone 6 added the usage and handoff documentation under `docs/specs` and linked it from `README.md`. Milestone 7 completed final verification and evidence capture.

Final verification is green: `go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service` passed, and `go test ./...` passed across the repository. No behavioral regressions or new surprises were observed during the closing verification run.

## Context and Orientation

Inbound Feishu messages flow through handler and command service layers, then into sender:

- `pkg/handler/message_handler.go` normalizes incoming Feishu events.
- `pkg/service/message_service.go` routes command and follow-up logic, and marks Codex responses with `render_mode=codex_markdown`.
- `pkg/sender/text_sender.go` currently performs delivery concerns: render-mode selection, plain-text chunking, reply-in-thread sends, and per-chunk interactive fallback.
- `pkg/feishumarkdown/prepare.go`, `pkg/feishumarkdown/translator.go`, and `pkg/feishumarkdown/splitter.go` now own Codex-markdown translation, markdown-aware splitting, and ordered chunk preparation.

Behavior-focused compatibility tests now live primarily under `pkg/feishumarkdown/*_test.go`, while `pkg/sender/text_sender_test.go` focuses on sender-specific delivery responsibilities.

Canonical runtime behavior for Feishu command flow and sender formatting is documented in `docs/specs/2026-03-17-feishu-mcp-command-format.md` and must remain authoritative.

## Plan of Work

This plan is intentionally split into seven milestones, each kept under 1000 changed LOC (adds + deletes) to keep review and rollback simple.

### Milestone 1: Package shell and public API contract (target 250-450 LOC)

Create a new package at `pkg/feishumarkdown` and define the external API contract without moving most implementation yet. Add `doc.go` and `prepare.go` with exported types and function signatures:

    const DefaultMaxChunkRunes = 1380

    type PrepareOptions struct {
        MaxChunkRunes int
    }

    type Chunk struct {
        Index   int
        Total   int
        Content string
    }

    type PreparedOutput struct {
        Translated string
        Chunks     []Chunk
    }

    func PrepareCodexMarkdown(input string, opts PrepareOptions) (PreparedOutput, error)

Add minimal tests in `pkg/feishumarkdown/prepare_test.go` for default option behavior and output shape contracts.

### Milestone 2: Translator runtime extraction with parity (target 600-850 LOC)

Move translator logic from `pkg/sender/markdown_translator.go` into `pkg/feishumarkdown/translator.go`. Keep all compatibility rules unchanged, including heading normalization, image/link degradation behavior, HTML escaping, markdown fence unwrapping, and task list rendering behavior.

At this stage, sender may still call old internals through thin wrappers if needed to keep diff small. The milestone is complete when package-level translator tests prove parity and no runtime behavior changes are observed in sender-facing tests.

### Milestone 3: Markdown splitter and chunk assembly extraction (target 550-900 LOC)

Move markdown chunk splitting logic from `pkg/sender/text_sender.go` into `pkg/feishumarkdown/splitter.go`, then compose translation + splitting + `[i/n]` suffix assembly inside `PrepareCodexMarkdown`.

Preserve all existing chunking invariants:

1. Do not split fenced code blocks unless they exceed cap, and keep balanced fences.
2. Keep table header and separator in the same chunk.
3. Keep section heading attached to following table/list/code block when possible.
4. Keep markdown chunk ordering markers as suffixes to preserve heading rendering.

### Milestone 4: Sender integration and deduplication (target 450-800 LOC)

Refactor `pkg/sender/text_sender.go` to use `feishumarkdown.PrepareCodexMarkdown` for codex markdown mode. Keep plain-text flow untouched.

Retain existing interactive send and fallback semantics:

1. First attempt `interactive` for codex markdown chunks.
2. On per-chunk interactive failure, retry once as plain `text`.

Delete now-duplicated codex markdown helpers from sender after tests pass.

### Milestone 5: Test migration and balancing (target 650-950 LOC)

Move behavior-focused markdown translator/splitter tests into `pkg/feishumarkdown` tests. Keep sender tests focused on sender concerns (message type, fallback, reply-in-thread behavior, API error propagation), not parser internals.

During migration, maintain coverage for every previously asserted compatibility rule. If any test must be renamed due to package movement, preserve assertion intent and inputs so regressions remain detectable.

### Milestone 6: Documentation and one-prompt handoff (target 250-500 LOC)

Create two documents under `docs/specs`:

1. `docs/specs/2026-03-20-feishu-markdown-library-usage.md` with full API usage, options, expected outputs, integration examples, and verification checklist.
2. `docs/specs/2026-03-20-feishu-markdown-library-agent-prompt.md` containing one model-agnostic copy-paste prompt that directs another coding agent to integrate this library in a target Go project.

Update `README.md` with a concise quickstart and links to those two docs.

### Milestone 7: Verification and evidence capture (target 100-250 LOC)

Run complete tests and capture concise evidence in this plan:

1. `go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service`
2. `go test ./...`

Optionally run simulation validation for confidence:

    FRIEREN_SIMULATION_MODE=1 FRIEREN_SIMULATION_REAL_MCP=1 FRIEREN_SIMULATION_ROUNDS=3 ./bin/frieren -config example.toml

Record observed pass results and any deviations in `Surprises & Discoveries` and `Outcomes & Retrospective`.

## Concrete Steps

All commands run from:

    /Users/jujiajia/code/frieren-clone

For each milestone, use this control loop before proceeding:

1. Implement only milestone-scoped edits.
2. Run milestone-targeted tests.
3. Check diff size:

       git diff --shortstat

4. If current milestone exceeds about 900 changed LOC, split that milestone into two smaller commits before continuing.
5. Commit milestone changes with concise diff-based commit message.

Suggested verification cadence:

    go test ./pkg/feishumarkdown ./pkg/sender
    go test ./pkg/service
    go test ./...

Milestone 4 evidence captured during execution:

    go test ./pkg/sender
    # github.com/D3Hunter/frieren-clone/pkg/sender [github.com/D3Hunter/frieren-clone/pkg/sender.test]
    pkg/sender/text_sender_test.go:557:21: undefined: prepareCodexMarkdown
    FAIL    github.com/D3Hunter/frieren-clone/pkg/sender [build failed]

    go test ./pkg/sender
    ok      github.com/D3Hunter/frieren-clone/pkg/sender  0.488s

    go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service
    ok      github.com/D3Hunter/frieren-clone/pkg/feishumarkdown   (cached)
    ok      github.com/D3Hunter/frieren-clone/pkg/sender           (cached)
    ok      github.com/D3Hunter/frieren-clone/pkg/service          (cached)

Milestone 5 evidence captured during execution:

    go test ./pkg/sender
    # github.com/D3Hunter/frieren-clone/pkg/sender [github.com/D3Hunter/frieren-clone/pkg/sender.test]
    pkg/sender/text_sender_test.go:9:2: "unicode/utf8" imported and not used
    FAIL    github.com/D3Hunter/frieren-clone/pkg/sender [build failed]

    go test ./pkg/feishumarkdown
    ok      github.com/D3Hunter/frieren-clone/pkg/feishumarkdown   0.522s

    go test ./pkg/sender
    ok      github.com/D3Hunter/frieren-clone/pkg/sender           1.143s

    go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service
    ok      github.com/D3Hunter/frieren-clone/pkg/feishumarkdown   (cached)
    ok      github.com/D3Hunter/frieren-clone/pkg/sender           0.531s
    ok      github.com/D3Hunter/frieren-clone/pkg/service          (cached)

## Validation and Acceptance

The implementation is accepted when all conditions are true:

1. A reusable package `pkg/feishumarkdown` exists with exported one-call API `PrepareCodexMarkdown`.
2. Sender codex markdown flow uses that API and no longer duplicates translator/splitter internals.
3. Existing codex markdown behavior remains functionally unchanged in tests.
4. Each implementation batch stays under 1000 changed LOC.
5. Full test suite passes (`go test ./...`).
6. Usage docs and one-prompt integration doc exist under `docs/specs` and are sufficient for another project/agent to adopt the library without repo-specific hidden context.

## Idempotence and Recovery

This work is additive and can be repeated safely. If a milestone fails tests, revert only that milestone commit and re-run the same milestone with smaller scope. Because each milestone is bounded under 1000 LOC, rollback impact stays localized.

If test migration temporarily reduces coverage, do not proceed to next milestone until moved tests are green and old duplicate tests are either retained or intentionally removed with equivalent replacements.

## Artifacts and Notes

Expected final artifacts:

1. New package files under `pkg/feishumarkdown`.
2. Updated sender integration in `pkg/sender/text_sender.go`.
3. Rebalanced tests in `pkg/feishumarkdown` and `pkg/sender`.
4. Docs:
   - `docs/specs/2026-03-20-feishu-markdown-library-usage.md`
   - `docs/specs/2026-03-20-feishu-markdown-library-agent-prompt.md`

Expected evidence snippets to capture during execution:

    go test ./...  # pass
    git diff --shortstat  # per-milestone LOC budget confirmation

    go test ./pkg/feishumarkdown
    --- FAIL: TestPrepareCodexMarkdown_TranslatesMarkdownForFeishu (0.00s)
        prepare_test.go:53: expected translated heading output, got "# Title"

    go test ./pkg/feishumarkdown ./pkg/sender
    ok  	github.com/D3Hunter/frieren-clone/pkg/feishumarkdown	0.815s
    ok  	github.com/D3Hunter/frieren-clone/pkg/sender	1.384s

## Interfaces and Dependencies

New package interface:

    package feishumarkdown

    const DefaultMaxChunkRunes = 1380

    type PrepareOptions struct {
        MaxChunkRunes int
    }

    type Chunk struct {
        Index   int
        Total   int
        Content string
    }

    type PreparedOutput struct {
        Translated string
        Chunks     []Chunk
    }

    func PrepareCodexMarkdown(input string, opts PrepareOptions) (PreparedOutput, error)

Dependency policy:

1. Continue using existing `github.com/yuin/goldmark` and GFM extension stack already in module.
2. Do not add new parser dependencies unless a parity bug cannot be solved with current stack.
3. Keep `pkg/service` render-mode contract unchanged (`codex_markdown` still selected there).

Compatibility policy:

1. This extraction is refactor-only by default.
2. Any unavoidable behavior drift must be documented in `Decision Log`, reflected in tests, and called out in `Outcomes & Retrospective`.

Revision note (2026-03-20 16:42 CST): Updated the living sections after Milestone 2 implementation to record the translator extraction, the temporary sender wrapper decision, and the red-green verification evidence.
Revision note (2026-03-20 17:36 CST): Updated the living sections after Milestone 4 implementation to record the sender integration, the new sender test seam, the explicit markdown-cap decision, and the red-green verification evidence.
Revision note (2026-03-20 18:24 CST): Updated the living sections after Milestone 5 implementation to record the test rebalance, the sender-wrapper removal, the suffix-budget discovery, and the verification evidence.
Revision note (2026-03-20 18:00 CST): Completed final verification (`go test ./pkg/feishumarkdown ./pkg/sender ./pkg/service` and `go test ./...`) and marked the ExecPlan terminal state.

---

Revision note (2026-03-20, Codex): Initial version created to execute markdown-library extraction in sub-1000-LOC milestones, per user request for smaller subtasks and repository ExecPlan rules.
Revision note (2026-03-20, Codex): Updated Milestone 1 execution status, captured testability discovery, recorded helper-based default-normalization decision, and added milestone outcome summary with verification evidence.
