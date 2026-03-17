# Feishu Long-Connection Client Design

**Date:** 2026-03-17  
**Project:** `frieren-clone`  
**Goal:** Build a minimal, extensible Feishu bot client using long connection that can receive message events and reply with text through Feishu message API v2.0.

## 1. Scope and Non-Goals

### In Scope (v1)
- Long-connection event receiving with Feishu server-side Go SDK.
- Handle `im.message.receive_v1` events.
- Parse text messages and reply using Feishu message API v2.0.
- TOML-based configuration.
- Buildable binary via `Makefile`.
- Project structure with `./cmd`, `./pkg`, and `example.toml`.

### Out of Scope (v1)
- Rich message/card replies.
- Multi-event plugin framework.
- Persistent storage and advanced workflows.
- Complex command routing.

## 2. High-Level Architecture

The system uses a thin startup layer in `cmd` and focused modules in `pkg`:

- `cmd/frieren/main.go`:
  - Load config from TOML.
  - Initialize Feishu clients.
  - Build dependencies (sender/service/handler).
  - Start long-connection client and block.

- `pkg/config`:
  - Load TOML file into typed config.
  - Validate required fields (`app.id`, `app.secret`).

- `pkg/feishu`:
  - Create base Feishu app client.
  - Create long-connection event dispatcher/client.
  - Provide IM v2 sender dependency.

- `pkg/handler`:
  - Receive `im.message.receive_v1` callbacks.
  - Filter unsupported/non-text/bot-origin messages.
  - Convert event payload into service input.
  - Call service and send reply.

- `pkg/service`:
  - Encapsulate message processing policy.
  - v1 behavior:
    - `echo_mode = true`: reply with incoming text.
    - `echo_mode = false`: reply with configured default text.

- `pkg/sender`:
  - Send text messages through IM v2 `messages.create`.

## 3. Directory and File Plan

- `cmd/frieren/main.go`
- `pkg/config/config.go`
- `pkg/config/load.go`
- `pkg/feishu/client.go`
- `pkg/handler/message_handler.go`
- `pkg/service/message_service.go`
- `pkg/sender/text_sender.go`
- `example.toml`
- `Makefile`

This keeps each file focused and easy to extend later.

## 4. Configuration Design (TOML)

`example.toml` shape:

```toml
[app]
id = "cli_xxx"
secret = "xxx"

[long_conn]
log_level = "info"
auto_reconnect = true
reconnect_backoff_sec = 3

[message]
default_reply = "收到，你的消息已处理。"
echo_mode = true
ignore_bot_messages = true

[logging]
level = "info"
```

Notes:
- `app` is required.
- `message.echo_mode` controls reply strategy.
- `message.ignore_bot_messages` avoids bot loops.
- `long_conn` reconnect fields are available as config knobs for runtime behavior and future expansion.

## 5. Data Flow

1. `main` loads config and initializes clients.
2. Long-connection receives `im.message.receive_v1`.
3. Handler extracts `chat_id`, `message_type`, content, sender info.
4. If non-text or ignored sender type, handler exits safely.
5. Handler calls `service.ProcessMessage(...)` for reply text.
6. Sender calls IM v2 API with `receive_id_type=chat_id` to send text reply.

## 6. Error Handling and Logging

- Config load/validation error:
  - Fail fast at startup with explicit missing field details.
- Event parse errors:
  - Log and continue loop.
- API send failures:
  - Log structured context (`chat_id`, `message_id`, error) and continue.
- Long-connection runtime errors:
  - Use SDK reconnect behavior; keep process alive unless unrecoverable init failure.

Logging levels:
- `info`: startup, connected, message handled, reply sent.
- `debug`: event details useful for diagnostics (without leaking secrets).
- `error`: failed parsing, send failures, init failures.

## 7. Makefile Targets

- `make build`: build binary to `./bin/frieren`
- `make run`: run app with `CONFIG=example.toml` default
- `make fmt`: `go fmt ./...`
- `make test`: `go test ./...`
- `make clean`: remove `./bin`

## 8. Acceptance Criteria

- App starts from TOML config and establishes long connection.
- Sending a text message in visible chat triggers bot text reply within a few seconds.
- Non-text messages do not crash the process.
- `make build` produces runnable binary.
- `make test` passes.

## 9. Extension Path

Future message workflows should be implemented in `pkg/service` first, keeping handler thin and transport-focused. This allows adding validation, command routing, or external API processing without rewriting startup and SDK integration.
