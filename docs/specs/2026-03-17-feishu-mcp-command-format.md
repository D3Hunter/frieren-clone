# Feishu MCP Command Format and Topic Thread Binding

Date: 2026-03-17
Status: Implemented (source-of-truth as-built spec)

Related references:

- `docs/specs/2026-03-19-feishu-mcp-id-glossary.md` (ID glossary and thread/topic mapping walkthrough)

## 1) Goal and scope

Convert the bot from simple echo/default-reply mode into a command-driven workflow that:

- supports MCP tool interaction via local streamable HTTP endpoint (`http://localhost:8787/mcp` by default),
- supports project-scoped Codex execution through `/<project> <prompt>`,
- persists Feishu topic to Codex thread bindings so follow-up replies in the same topic continue the same Codex thread,
- provides user-visible progress feedback (immediate `OnIt` reaction + periodic heartbeat),
- keeps replies in the same topic chain and handles long outputs robustly.

## 2) Canonical command surface

The command set is:

- `/help`
- `/mcp tools`
- `/mcp schema <tool>`
- `/mcp call <tool> <json>`
- `/<project> <prompt>`

Behavior notes:

- `/mcp call` currently accepts only a JSON object payload.
- `/mcp call codex` is treated as a fresh-run command and starts a new Codex thread each time.
- Rationale: this is intentional so one Feishu topic can host multiple independent Codex threads when users want parallel or reset investigations.
- Any slash command that starts Codex execution (`/mcp call codex`, `/<project> <prompt>`) always creates a new thread and resets the topic binding to that latest thread.
- Plain-text follow-up messages in the same topic always continue the latest bound Codex thread.
- `/help` includes a one-line reminder of this `/mcp call codex` new-thread behavior.
- Unknown commands fall back to concise help.
- Unknown project alias returns `Unknown project alias: <alias>`.

## 3) Trigger and routing rules

Routing is implemented in `pkg/service/message_service.go` and fed by `pkg/handler/message_handler.go`.

### Group-chat trigger rule

In group/topic-group chats, **slash commands require mention**:

- Only messages that are both slash commands and include bot mention are treated as new commands.
- Mention identity is checked via incoming mention open_id list against `commands.bot_open_id`.

If a group slash command lacks mention, bot returns usage reminder.

### Topic follow-up rule

If a message is plain text (not slash) and belongs to a topic with existing binding:

- the message is treated as follow-up,
- bot reuses bound Codex thread id,
- bot replies in same topic chain,
- binding timestamp is refreshed.

### Outside bound topic context

Plain text outside bound topic context does not use legacy echo behavior; it returns concise help.

## 4) Architecture and data flow

1. `pkg/handler/message_handler.go`
   - accepts Feishu `im.message.receive_v1`,
   - filters non-text and (optional) bot-origin messages,
   - extracts: `chat_id`, `message_id`, `thread_id`, `chat_type`, raw text, mention open_ids,
   - forwards to service layer.

2. `pkg/service/message_service.go`
   - parses command/follow-up intent,
   - adds immediate `OnIt` reaction on the user message,
   - runs command with heartbeat ticker,
   - sends final response,
   - persists topic binding when project-thread is known.

3. `pkg/sender/text_sender.go`
   - replies to incoming message via Feishu reply API (`reply_in_thread=true`),
   - uses `text` message type to avoid extra post title formatting,
   - chunks long outputs and prefixes ordering markers.

4. `pkg/runtime/topic_state_store.go`
   - persists topic->thread binding JSON to disk,
   - reloads bindings at startup.

5. `pkg/mcp/adapter.go`
   - creates short-lived go-sdk session per MCP command,
   - lists tools, resolves schema, executes call.

6. `pkg/service/message_service.go`
   - starts Codex sessions through MCP `codex` tool for slash commands,
   - continues Codex sessions through MCP `codex-reply` tool for plain-text topic follow-ups,
   - parses tool output text to extract `threadId` and persist latest topic binding.

## 5) Config contract

Defined in `pkg/config/config.go`.

```toml
[mcp]
endpoint = "http://localhost:8787/mcp"
timeout_sec = 30

[commands]
bot_open_id = "ou_bot_open_id"
heartbeat_sec = 180
start_reaction = "OnIt"

[runtime]
topic_state_file = ".state/topic-state.json"

[projects.<alias>]
cwd = "/abs/path/to/project"
```

Defaults/validation:

- `mcp.endpoint` default: `http://localhost:8787/mcp`
- `mcp.timeout_sec` default: `30` (used for non-Codex MCP operations; `codex`/`codex-reply` calls are long-running and do not use this per-call timeout)
- `commands.heartbeat_sec` default: `180`
- `commands.start_reaction` default: `OnIt`
- `runtime.topic_state_file` default: `.state/topic-state.json`
- `projects.<alias>.cwd` required when alias exists
- project aliases are normalized to lowercase

## 6) MCP integration details

Implementation: `pkg/mcp/adapter.go`.

- SDK: `github.com/modelcontextprotocol/go-sdk v1.3.1` (compatible with Go 1.23.2 in this repo)
- Transport: `mcp.StreamableClientTransport`
- Session policy: **reuse one long-lived MCP session per bot process, close on shutdown**
  - Rationale: session-scoped tool pairs like `codex` + `codex-reply` rely on the same MCP session context across follow-up calls.
  - Idle timeout: MCP session is rotated after 1 hour of inactivity to avoid keeping stale sessions indefinitely.
- Operations:
  - `/mcp tools` -> `ListTools` (cursor loop)
  - `/mcp schema <tool>` -> list tools and print target `inputSchema`
  - `/mcp call <tool> <json>` -> `CallTool`
- Timeout behavior:
  - non-Codex tool calls use `mcp.timeout_sec`
  - `codex` and `codex-reply` are not bounded by `mcp.timeout_sec` so long-running Codex execution is not cut off at 30s
- Tool-level `isError=true` is surfaced as command failure text.

No local MCP schema/tool cache is used in current design.

## 7) Codex integration details

Implementation: `pkg/service/message_service.go` with MCP tools exposed by local MCP server.

### New thread (`/<project> <prompt>` and `/mcp call codex ...`)

Uses MCP `codex` tool with defaults auto-filled when missing:

- model default `gpt-5.3-codex`
- sandbox default `danger-full-access`
- `approval-policy="never"`
- `config.model_reasoning_effort="xhigh"`

`threadId`/`conversationId` fields are removed for `/mcp call codex` so slash Codex commands always start a fresh thread.

### Existing thread (topic follow-up)

Uses MCP `codex-reply` tool:

- `threadId=<bound_codex_thread_id>`
- `prompt=<plain_text_followup>`

Final visible message is taken from MCP tool textual output; thread id is parsed from output JSON fragment when present, otherwise previous bound id is retained.

For every successful Codex execution response (`codex` or `codex-reply`), service also calls MCP `codex-status` with:

- `threadId=<resolved_codex_thread_id>`

When `codex-status` returns parseable context window usage, final footer includes:

- `context_window: <usedK> / <maxK> tokens used (<left%> left)`
- `codex_thread_id: <thread_id>`

If `codex-status` is unavailable or its output is not parseable, reply still includes `codex_thread_id` footer and skips `context_window`.

If follow-up `codex-reply` returns session-not-found (for example, after MCP session idle-timeout rotation):

- bot first posts a topic notice explaining this follow-up will run in a new Codex session,
- notice includes environment snapshot (`project_alias`, previous `codex_thread_id`, `cwd`, topic id, chat id),
- bot then starts a fresh `codex` run with follow-up prompt (and `cwd` when project alias maps to one),
- topic binding is refreshed to the new thread id.

## 8) Topic-state persistence model

Implemented in `pkg/runtime/topic_state_store.go`.

Binding key:

- `chat_id + feishu_thread_id`

Binding value:

- `project_alias`
- `codex_thread_id`
- `updated_at`

Stored as JSON array in configured `runtime.topic_state_file` path.

Example payload:

```json
{
  "bindings": [
    {
      "chat_id": "oc_xxx",
      "feishu_thread_id": "omt_xxx",
      "project_alias": "tidb",
      "codex_thread_id": "019c...",
      "updated_at": "2026-03-17T06:00:00Z"
    }
  ]
}
```

## 9) Sender behavior: topic reply, formatting, chunking, heartbeat

Implemented in `pkg/sender/text_sender.go` and service heartbeat flow.

- All service responses are sent as replies to incoming message id.
- Reply API uses `reply_in_thread=true` to keep same topic chain behavior.
- Content mode selection:
  - always uses `text` to avoid extra post title rendering (for example bold `Frieren` title).
- Codex footer formatting:
  - appends thread metadata section at the bottom as plain text,
  - includes `codex_thread_id`,
  - includes `context_window` token usage line when `codex-status` succeeds and usage can be parsed.
- Long output splitting:
  - chunk at 1800 runes,
  - each chunk prefixed with `[i/n]`.
- Processing feedback:
  - add immediate `OnIt` reaction on incoming user message,
  - send `Still processing (elapsed XmYYs), please wait...` every configured heartbeat interval (default 180 seconds) until completion.

## 10) Verification and test coverage

Implemented tests include:

- `pkg/config/config_test.go`
- `pkg/runtime/topic_state_store_test.go`
- `pkg/mcp/adapter_test.go`
- `pkg/service/command_service_test.go`
- `pkg/handler/message_handler_test.go`
- `pkg/sender/text_sender_test.go`

Repository verification command:

- `go test ./...`

(Last run after implementation on this branch: pass.)

## 11) Known limitations / follow-ups

- `message.echo_mode` remains in config for backward compatibility but is not the primary runtime behavior for command flow.
- Group slash trigger relies on mention open_id extraction from Feishu event mentions.
- `/mcp call` supports JSON object payload only (by design for v1 command clarity).
- No MCP tool/schema cache yet.
- Codex execution now depends on MCP server availability and exposed tools (`codex`, `codex-reply`).
