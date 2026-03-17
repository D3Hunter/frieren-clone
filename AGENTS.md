# Agent Notes

Before changing Feishu bot command behavior, MCP integration, topic binding, or sender formatting,
read `/Users/jujiajia/code/frieren-clone/docs/specs/2026-03-17-feishu-mcp-command-format.md` first.

Treat that spec as the canonical source of truth for command grammar, routing rules,
state persistence, and runtime defaults.

# ExecPlans

When writing complex features or significant refactors, use an ExecPlan
as described in `PLANS.md` from design to implementation.

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
