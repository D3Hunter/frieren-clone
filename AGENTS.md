# Agent Notes

Before changing Feishu bot command behavior, MCP integration, topic binding, or sender formatting,
read `/Users/jujiajia/code/frieren-clone/docs/specs/2026-03-17-feishu-mcp-command-format.md` first.

Treat that spec as the canonical source of truth for command grammar, routing rules,
state persistence, and runtime defaults.

# ExecPlans

When writing complex features or significant refactors, use an ExecPlan
as described in `PLANS.md` from design to implementation.

Plan files must be saved under `/Users/jujiajia/code/frieren-clone/docs/specs`.
This repository rule overrides any default plan/spec output location suggested by skills
(for example `writing-plans` defaults like `docs/superpowers/plans`) unless the user explicitly requests otherwise.

# Logging Style

- Use `logger.With(...)` only for context fields that are shared across multiple log lines, and reuse that derived logger.
- For one-off fields, pass them directly on the log call (for example `logger.Info("...", zap.Int("x", x))`) instead of creating a one-time `.With(...)`.
- Keep per-line fields focused on event-specific details (for example `err`, `elapsed`, `tool`, `text`) to improve readability.
- Prefer this pattern in handler/service flows that carry repeated message metadata (`chat_id`, `message_id`, `thread_id`, `request_id`, `correlation_id`).

# Go Doc Comments

- All top-level exported objects (types, funcs, methods, consts, vars) must have doc comments.
- Each comment must start with the target name (for example `Config ...`, `HandleEvent ...`, `FormatJSON ...`).
- Every interface method declaration must also have a comment line, and the comment must start with that method name.
- Comments should explain what the target does and, when useful, how it does it.
- Keep comment length proportional to complexity: simple wrappers can use one sentence; workflow-heavy logic should use richer context.

# Special Handling Branches

- When adding special-case handling branches (for example compatibility fallbacks, provider quirks, or non-obvious guard paths), add an in-code comment that explains why the branch exists.

# PR Workflow

- Do not use a `[codex]` prefix in PR titles.
- When filing a PR, generate the title and description from the actual diff against `main`.
- Keep PR descriptions short and include only:
  1. A brief summary of what changed.
  2. A brief summary of tests that were added.
- Do not include process/meta notes in PR descriptions (for example, notes about intentionally omitting the `[codex]` prefix).
- After merging a PR and rolling out to a new workspace:
  1. Run `git fetch --prune` to remove stale remote branches.
  2. Pull the latest `main`.
  3. Switch to a fresh new working branch created from the updated `main`.

# Shortcut Prompt

- If the user's message clearly means "ship and reset" (for example `ship-and-reset`, `ship and reset`, `ship/reset`, `commit pr merge cleanup`, or close variants), run the full integration flow in one pass:
  1. Run verification (`go test ./...`) and stop if it fails.
  2. Stage all changes, commit with a concise diff-based message, and push the current branch.
  3. Open a PR against `main` with title/description derived from the actual diff.
  4. Merge the PR.
  5. Run cleanup: `git fetch --prune`, pull latest `main`, and switch to a fresh `codex/` branch from updated `main`.
