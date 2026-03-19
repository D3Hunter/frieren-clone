# Feishu and MCP ID Glossary (Thread/Topic Mapping)

Date: 2026-03-19
Status: Reference

## 1) Why this doc exists

Feishu and MCP both use the word "thread", but they are different ID namespaces:

- Feishu topic thread id (conversation container in Feishu)
- Codex/MCP thread id (execution/session context in MCP toolchain)

This document defines each ID, where it comes from, and how they are connected at runtime.

## 2) ID glossary

| Name in docs | Common field names in code/logs | Layer | Example prefix | Set by | Meaning |
| --- | --- | --- | --- | --- | --- |
| Feishu chat id | `chat_id`, `ChatID` | Feishu event + runtime | `oc_` | Feishu | The chat container (group/private chat). |
| Feishu message id | `message_id`, `MessageID` | Feishu event + runtime | `om_` | Feishu | The specific incoming message. |
| Feishu topic id | `thread_id`, `topic_id`, `FeishuThreadID`, `ThreadID` (in inbound message context) | Feishu event + sender receipt + runtime state | `omt_` | Feishu | Topic/thread chain in Feishu where replies are grouped. |
| Codex thread id | `codex_thread_id`, `CodexThreadID`, MCP arg `threadId` | MCP output + runtime state | UUID-like | MCP codex tools | Execution/session thread context for `codex` + `codex-reply`. |
| Project alias | `project_alias`, `ProjectAlias` | Command routing + runtime state | `tidb`, etc. | User command / service | Alias used to resolve project `cwd`. |
| Bot open id | `bot_open_id` | Config + mention matching | `ou_` | Feishu/config | Bot identity used to check mentions in group slash commands. |
| Mentioned open ids | `mentioned_ids`, `MentionedIDs` | Feishu event parsing | `ou_` | Feishu | Mentioned users extracted from message mentions. |
| Request id | `request_id`, `RequestID` | Internal tracing | `req_...` | Service | Per-message trace id for logs/diagnostics. |
| Correlation id | `correlation_id`, `CorrelationID` | Internal tracing | `corr_...` | Service | Stable trace key across handling path. |

## 3) Canonical binding model

Binding key:

- `chat_id + feishu_thread_id`

Binding value:

- `project_alias`
- `codex_thread_id`
- `updated_at`

Persistence model:

- Saved to `runtime.topic_state_file` JSON (`.state/topic-state.json` by default).
- One binding per `(chat_id, feishu_thread_id)`.

Important: Feishu topic ids and Codex thread ids are never compared directly; they are only linked through this binding table.

## 4) Lifecycle: "no topic initially -> bot reply creates topic -> user follows up in topic"

### Step A: User sends first command message without topic id

- Incoming event may have empty `thread_id`.
- Service still runs command and calls MCP `codex` (new thread flow).

### Step B: Bot reply causes Feishu topic to exist

- Sender replies to original message with `reply_in_thread=true`.
- Feishu reply API returns `resp.Data.ThreadId` (topic id, usually `omt_*`).

### Step C: Service persists mapping

- Service computes Feishu topic id with:
  - `chooseThreadID(msg.ThreadID, finalReceipt.ThreadID)`
- If inbound `msg.ThreadID` was empty, it falls back to `finalReceipt.ThreadID` (from Feishu reply result).
- Service parses `codex_thread_id` from MCP tool output and upserts:
  - key: `(chat_id, feishu_thread_id)`
  - value: `(project_alias, codex_thread_id)`

### Step D: User replies in that topic

- Incoming follow-up now includes Feishu `thread_id=omt_*`.
- Service looks up `(chat_id, thread_id)` in topic store.
- If binding exists, service calls MCP `codex-reply` with:
  - `threadId=<bound codex_thread_id>`
  - `prompt=<follow-up plain text>`

### Step E: Service refreshes mapping

- After successful follow-up execution, service refreshes binding `updated_at`.
- If returned output carries a new `threadId`, mapping updates to latest `codex_thread_id`.

## 5) Field naming rules for humans (recommended reading style)

When reading code/logs/specs, mentally apply these rules:

1. `thread_id` in Feishu event/sender context means Feishu topic id (`omt_*`).
2. `threadId` argument for MCP `codex-reply` means Codex session thread id.
3. `topic_id` in logs is currently an alias of Feishu `thread_id` for readability.
4. `codex_thread_id` always means MCP/Codex thread id; this one is never a Feishu id.

Quick mnemonic:

- `omt_*` -> Feishu topic
- `codex_*` or UUID-like thread id -> MCP/Codex thread

## 6) Code anchors

Primary flow anchors:

- Handler extracts incoming Feishu `thread_id`:
  - `pkg/handler/message_handler.go`
- Service resolves and persists topic binding:
  - `pkg/service/message_service.go`
- Sender gets reply `ThreadId` from Feishu API:
  - `pkg/sender/text_sender.go`
- Runtime store key/value persistence:
  - `pkg/runtime/topic_state_store.go`

Related tests for this lifecycle:

- `pkg/service/command_service_test.go`:
  - `TestHandleIncomingMessage_ProjectCommandBindsTopic`
  - `TestHandleIncomingMessage_TopicFollowupUsesBoundThread`
