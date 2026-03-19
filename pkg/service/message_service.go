package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
)

// ToolInfo is the command-service view of an MCP tool.
type ToolInfo struct {
	Name        string
	Description string
}

// MCPGateway describes MCP operations needed by CommandService.
type MCPGateway interface {
	// ListTools returns currently available MCP tools.
	ListTools(ctx context.Context) ([]ToolInfo, error)
	// GetToolSchema returns a printable input schema for one named MCP tool.
	GetToolSchema(ctx context.Context, tool string) (string, error)
	// CallTool executes one MCP tool with decoded JSON arguments.
	CallTool(ctx context.Context, tool string, args map[string]any) (string, error)
}

// TopicBinding links a Feishu topic thread with a project alias and Codex thread ID.
type TopicBinding struct {
	ChatID         string
	FeishuThreadID string
	ProjectAlias   string
	CodexThreadID  string
}

// TopicStore persists and loads topic bindings for follow-up routing.
type TopicStore interface {
	// Get looks up a saved binding by Feishu chat and thread IDs.
	Get(chatID, feishuThreadID string) (TopicBinding, bool)
	// Upsert inserts or updates one topic binding entry.
	Upsert(binding TopicBinding) error
}

// OutgoingMessage describes one response message sent back to Feishu.
type OutgoingMessage struct {
	ChatID           string
	ReplyToMessageID string
	ThreadID         string
	Text             string
	RenderMode       string
}

// OutgoingReaction describes one emoji reaction to add on a user message.
type OutgoingReaction struct {
	MessageID string
	EmojiType string
}

// SendReceipt carries metadata returned after sending an outgoing message.
type SendReceipt struct {
	ThreadID string
}

// MessageSender is the outbound transport CommandService uses for replies and reactions.
type MessageSender interface {
	// Send sends one outgoing message and returns delivery metadata.
	Send(ctx context.Context, msg OutgoingMessage) (SendReceipt, error)
	// AddReaction adds one reaction to the source user message.
	AddReaction(ctx context.Context, reaction OutgoingReaction) error
}

// IncomingMessage is the normalized inbound message payload used by CommandService.
type IncomingMessage struct {
	ChatID        string
	MessageID     string
	ThreadID      string
	ChatType      string
	RawText       string
	MentionedIDs  []string
	RequestID     string
	CorrelationID string
}

// CommandServiceConfig controls command execution behavior and project alias resolution.
type CommandServiceConfig struct {
	BotOpenID               string
	Heartbeat               time.Duration
	StartProcessingReaction string
	ProjectAliasCWD         map[string]string
}

// CommandServiceDeps groups external dependencies used by NewCommandService.
type CommandServiceDeps struct {
	MCP        MCPGateway
	Sender     MessageSender
	TopicStore TopicStore
	Logger     *zap.Logger
	Config     CommandServiceConfig
}

// CommandService parses incoming messages and routes them to MCP/Codex workflows.
type CommandService struct {
	mcp        MCPGateway
	sender     MessageSender
	topicStore TopicStore
	cfg        CommandServiceConfig
	logger     *zap.Logger
}

var mentionTagPattern = regexp.MustCompile(`(?s)<at\b[^>]*>.*?</at>`)
var projectCommandPattern = regexp.MustCompile(`(?s)^/([a-zA-Z0-9_-]+)\s+(.+)$`)
var codexThreadIDPattern = regexp.MustCompile(`(?i)"thread(?:_|)id"\s*:\s*"([^"]+)"`)
var tokenUsagePattern = regexp.MustCompile(`(?i)([0-9][0-9,._]*)\s*/\s*([0-9][0-9,._]*)\s*(?:tokens?)`)

const (
	processingStartReactionType = "OnIt"
	codexToolName               = "codex"
	codexReplyToolName          = "codex-reply"
	codexStatusToolName         = "codex-status"
	mcpCodexTopicAlias          = "__mcp_codex__"
	renderModePlainText         = "plain_text"
	renderModeCodexMarkdown     = "codex_markdown"
	defaultHelpMessage          = "Available commands:\n/help\n/mcp tools\n/mcp schema <tool>\n/mcp call <tool> <json>\n/<project> <prompt>\n\nNote: /mcp call codex always starts a new Codex thread."
	codexPromptHelpMessage      = `Usage: /mcp call codex {"prompt":"<your prompt>"}`
	defaultCodexModel           = "gpt-5.3-codex"
	defaultCodexReasoningEffort = "xhigh"
	defaultCodexSandbox         = "danger-full-access"
	defaultCodexApprovalPolicy  = "never"
	// Intentionally keep /mcp call codex as "start new thread" so users can open
	// multiple independent Codex threads inside one Feishu topic when needed.
	codexNewThreadNotice     = "Note: by design, /mcp call codex always starts a new Codex thread and does not reuse the current topic binding."
	groupMentionHelpMessage  = "In group chats, mention the bot before slash commands, for example: @bot /help"
	unknownProjectHelpPrefix = "Unknown project alias"
	codexSessionTimeout      = time.Hour
)

// NewCommandService builds CommandService with normalized config and safe logger defaults.
func NewCommandService(deps CommandServiceDeps) *CommandService {
	cfg := deps.Config
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 3 * time.Minute
	}
	if cfg.ProjectAliasCWD == nil {
		cfg.ProjectAliasCWD = map[string]string{}
	}
	cfg.StartProcessingReaction = strings.TrimSpace(cfg.StartProcessingReaction)
	if cfg.StartProcessingReaction == "" {
		cfg.StartProcessingReaction = processingStartReactionType
	}
	normalized := make(map[string]string, len(cfg.ProjectAliasCWD))
	for alias, cwd := range cfg.ProjectAliasCWD {
		normalized[strings.ToLower(strings.TrimSpace(alias))] = strings.TrimSpace(cwd)
	}
	cfg.ProjectAliasCWD = normalized

	return &CommandService{
		mcp:        deps.MCP,
		sender:     deps.Sender,
		topicStore: deps.TopicStore,
		cfg:        cfg,
		logger:     logger,
	}
}

// HandleIncomingMessage parses one normalized inbound message and executes matching command flow.
func (s *CommandService) HandleIncomingMessage(ctx context.Context, msg IncomingMessage) error {
	msg.ChatID = strings.TrimSpace(msg.ChatID)
	msg.MessageID = strings.TrimSpace(msg.MessageID)
	msg.ThreadID = strings.TrimSpace(msg.ThreadID)
	msg = EnsureTraceIDs(msg)
	msgLogger := s.messageLogger(msg)
	if msg.ChatID == "" {
		return fmt.Errorf("chat id is required")
	}
	if msg.MessageID == "" {
		return fmt.Errorf("message id is required")
	}

	text := strings.TrimSpace(msg.RawText)
	msgLogger.Info("incoming feishu message", zap.String("raw_text", text))
	if text == "" {
		msgLogger.Info("incoming message has empty text")
		_, err := s.send(ctx, msg, "Please enter a command. Use /help for usage.", renderModePlainText)
		return err
	}
	cleanText := stripMentions(text)
	msgLogger.Info("parsed incoming message text", zap.String("clean_text", cleanText))

	binding, hasBinding := TopicBinding{}, false
	if msg.ThreadID != "" && s.topicStore != nil {
		binding, hasBinding = s.topicStore.Get(msg.ChatID, msg.ThreadID)
		if hasBinding && !s.bindingSupportsProjectFollowup(binding) {
			hasBinding = false
		}
	}
	if hasBinding {
		msgLogger.Info(
			"resolved topic binding",
			zap.String("project_alias", strings.TrimSpace(binding.ProjectAlias)),
			zap.String("codex_thread_id", strings.TrimSpace(binding.CodexThreadID)),
		)
	}

	if strings.HasPrefix(cleanText, "/") {
		if s.requiresMention(cleanText, msg) {
			msgLogger.Info("group slash command missing bot mention", zap.String("command_text", cleanText))
			_, err := s.send(ctx, msg, groupMentionHelpMessage, renderModePlainText)
			return err
		}
		return s.handleSlashCommand(ctx, msg, cleanText)
	}

	if hasBinding {
		return s.handleTopicFollowup(ctx, msg, cleanText, binding)
	}

	msgLogger.Info("plain text without topic binding; returning help")
	_, err := s.send(ctx, msg, "Use /help to see command syntax.", renderModePlainText)
	return err
}

func (s *CommandService) requiresMention(cleanText string, msg IncomingMessage) bool {
	if !strings.HasPrefix(cleanText, "/") {
		return false
	}
	if !isGroupChat(msg.ChatType) {
		return false
	}
	botID := strings.TrimSpace(s.cfg.BotOpenID)
	if botID == "" {
		return false
	}
	for _, id := range msg.MentionedIDs {
		if strings.TrimSpace(id) == botID {
			return false
		}
	}
	return true
}

func (s *CommandService) handleSlashCommand(ctx context.Context, msg IncomingMessage, cleanText string) error {
	msgLogger := s.messageLogger(msg)
	msgLogger.Info("handling slash command", zap.String("command_text", cleanText))
	switch {
	case cleanText == "/help":
		_, err := s.send(ctx, msg, defaultHelpMessage, renderModePlainText)
		return err
	case cleanText == "/mcp tools":
		return s.handleMCPTools(ctx, msg)
	case strings.HasPrefix(cleanText, "/mcp schema "):
		tool := strings.TrimSpace(strings.TrimPrefix(cleanText, "/mcp schema "))
		if tool == "" {
			_, err := s.send(ctx, msg, "Usage: /mcp schema <tool>", renderModePlainText)
			return err
		}
		return s.handleMCPSchema(ctx, msg, tool)
	case strings.HasPrefix(cleanText, "/mcp call "):
		tool, argsRaw, ok := parseMCPCallCommand(cleanText)
		if !ok {
			_, err := s.send(ctx, msg, "Usage: /mcp call <tool> <json>", renderModePlainText)
			return err
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
			_, sendErr := s.send(ctx, msg, attachDiagnosticID(fmt.Sprintf("JSON parse failed: %v", err), msg), renderModePlainText)
			if sendErr != nil {
				return sendErr
			}
			return nil
		}
		if args == nil {
			args = map[string]any{}
		}
		if strings.EqualFold(tool, codexToolName) && strings.TrimSpace(stringArg(args, "prompt")) == "" {
			_, err := s.send(ctx, msg, codexPromptHelpMessage, renderModePlainText)
			return err
		}
		return s.handleMCPCall(ctx, msg, tool, args)
	default:
		alias, prompt, ok := parseProjectCommand(cleanText)
		if !ok {
			_, err := s.send(ctx, msg, defaultHelpMessage, renderModePlainText)
			return err
		}
		msgLogger.Info(
			"handling project command",
			zap.String("project_alias", alias),
			zap.String("prompt", prompt),
		)
		cwd, ok := s.cfg.ProjectAliasCWD[alias]
		if !ok || strings.TrimSpace(cwd) == "" {
			_, err := s.send(ctx, msg, fmt.Sprintf("%s: %s", unknownProjectHelpPrefix, alias), renderModePlainText)
			return err
		}
		outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
			startArgs := ensureCodexStartDefaults(map[string]any{
				"cwd":    cwd,
				"prompt": prompt,
			})
			output, runErr := s.mcp.CallTool(runCtx, codexToolName, startArgs)
			if runErr != nil {
				return commandOutcome{}, runErr
			}
			codexThreadID := strings.TrimSpace(parseCodexThreadID(output))
			return commandOutcome{
				text:          output,
				codexThreadID: codexThreadID,
				tokenUsage:    s.resolveCodexContextWindowUsage(runCtx, codexThreadID, msgLogger),
				projectAlias:  alias,
			}, nil
		})
		if err != nil {
			return s.replyCommandFailure(ctx, msg, "Execution failed", err)
		}
		finalReceipt, err := s.send(ctx, msg, formatCodexOutput(outcome.text, outcome.codexThreadID, outcome.tokenUsage), renderModeCodexMarkdown)
		if err != nil {
			return err
		}
		if s.topicStore != nil {
			feishuThreadID := chooseThreadID(msg.ThreadID, finalReceipt.ThreadID)
			if feishuThreadID != "" && strings.TrimSpace(outcome.codexThreadID) != "" {
				bindingLogger := msgLogger.With(
					zap.String("topic_id", feishuThreadID),
					zap.String("project_alias", alias),
					zap.String("codex_thread_id", strings.TrimSpace(outcome.codexThreadID)),
				)
				if err := s.topicStore.Upsert(TopicBinding{
					ChatID:         msg.ChatID,
					FeishuThreadID: feishuThreadID,
					ProjectAlias:   alias,
					CodexThreadID:  outcome.codexThreadID,
				}); err != nil {
					bindingLogger.Error("persist topic binding failed", zap.Error(err))
					return err
				}
				bindingLogger.Info("persisted topic binding")
			}
		}
		return nil
	}
}

func (s *CommandService) handleMCPTools(ctx context.Context, msg IncomingMessage) error {
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		tools, runErr := s.mcp.ListTools(runCtx)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		if len(tools) == 0 {
			return commandOutcome{text: "No MCP tools are currently available."}, nil
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		lines := make([]string, 0, len(tools)+1)
		lines = append(lines, "Available MCP tools:")
		for _, tool := range tools {
			line := "- " + tool.Name
			if strings.TrimSpace(tool.Description) != "" {
				line += ": " + tool.Description
			}
			lines = append(lines, line)
		}
		return commandOutcome{text: strings.Join(lines, "\n")}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "Failed to list tools", err)
	}
	_, err = s.send(ctx, msg, normalizeOutput(outcome.text), renderModePlainText)
	return err
}

func (s *CommandService) handleMCPSchema(ctx context.Context, msg IncomingMessage, tool string) error {
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		schema, runErr := s.mcp.GetToolSchema(runCtx, tool)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		return commandOutcome{text: fmt.Sprintf("Schema for %s:\n%s", tool, schema)}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "Failed to get schema", err)
	}
	_, err = s.send(ctx, msg, normalizeOutput(outcome.text), renderModePlainText)
	return err
}

func (s *CommandService) handleMCPCall(ctx context.Context, msg IncomingMessage, tool string, args map[string]any) error {
	tool = strings.TrimSpace(tool)
	msgLogger := s.messageLogger(msg)
	msgLogger.Info("handling mcp call", zap.String("tool", tool), zap.Any("args", args))
	if strings.EqualFold(tool, codexToolName) {
		args = sanitizeCodexStartArgs(args)
	}
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		result, runErr := s.mcp.CallTool(runCtx, tool, args)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		codexThreadID := strings.TrimSpace(parseCodexThreadID(result))
		tokenUsage := ""
		if strings.EqualFold(tool, codexToolName) {
			tokenUsage = s.resolveCodexContextWindowUsage(runCtx, codexThreadID, msgLogger)
		}
		return commandOutcome{
			text:          result,
			codexThreadID: codexThreadID,
			tokenUsage:    tokenUsage,
		}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "Failed to call tool", err)
	}
	responseText := normalizeOutput(outcome.text)
	if strings.EqualFold(tool, codexToolName) {
		responseText = formatCodexOutput(outcome.text, outcome.codexThreadID, outcome.tokenUsage, codexNewThreadNotice)
	}
	renderMode := renderModePlainText
	if strings.EqualFold(tool, codexToolName) {
		renderMode = renderModeCodexMarkdown
	}
	finalReceipt, err := s.send(ctx, msg, responseText, renderMode)
	if err != nil {
		return err
	}
	if strings.EqualFold(tool, codexToolName) {
		s.recordCodexThreadID(msg, finalReceipt, outcome.text)
	}
	return nil
}

func (s *CommandService) handleTopicFollowup(ctx context.Context, msg IncomingMessage, cleanText string, binding TopicBinding) error {
	msgLogger := s.messageLogger(msg)
	threadID := strings.TrimSpace(binding.CodexThreadID)
	if threadID == "" {
		_, err := s.send(ctx, msg, "No Codex thread is bound to this topic yet. Send a new slash command first.", renderModePlainText)
		return err
	}
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		output, runErr := s.mcp.CallTool(runCtx, codexReplyToolName, map[string]any{
			"threadId": threadID,
			"prompt":   cleanText,
		})
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		resolvedThreadID := strings.TrimSpace(parseCodexThreadID(output))
		if resolvedThreadID == "" {
			resolvedThreadID = threadID
		}
		return commandOutcome{
			text:          output,
			codexThreadID: resolvedThreadID,
			tokenUsage:    s.resolveCodexContextWindowUsage(runCtx, resolvedThreadID, msgLogger),
			projectAlias:  strings.ToLower(strings.TrimSpace(binding.ProjectAlias)),
		}, nil
	})
	if err != nil {
		if !isCodexReplySessionNotFoundError(err) {
			return s.replyCommandFailure(ctx, msg, "Execution failed", err)
		}

		notice := s.formatSessionResetNotice(msg, binding)
		if _, noticeErr := s.send(ctx, msg, notice, renderModePlainText); noticeErr != nil {
			return noticeErr
		}
		msgLogger.Warn(
			"codex reply session not found; restarting with a new codex session",
			zap.String("project_alias", strings.ToLower(strings.TrimSpace(binding.ProjectAlias))),
			zap.String("previous_codex_thread_id", threadID),
		)

		outcome, err = s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
			nextOutcome, runErr := s.startNewCodexSessionFromFollowup(runCtx, cleanText, binding, threadID)
			if runErr != nil {
				return commandOutcome{}, runErr
			}
			nextOutcome.tokenUsage = s.resolveCodexContextWindowUsage(runCtx, nextOutcome.codexThreadID, msgLogger)
			return nextOutcome, nil
		})
		if err != nil {
			return s.replyCommandFailure(ctx, msg, "Execution failed", err)
		}
	}
	finalReceipt, err := s.send(ctx, msg, formatCodexOutput(outcome.text, outcome.codexThreadID, outcome.tokenUsage), renderModeCodexMarkdown)
	if err != nil {
		return err
	}
	if s.topicStore != nil {
		feishuThreadID := chooseThreadID(msg.ThreadID, finalReceipt.ThreadID)
		if feishuThreadID != "" {
			bindingLogger := msgLogger.With(
				zap.String("topic_id", feishuThreadID),
				zap.String("project_alias", strings.ToLower(strings.TrimSpace(outcome.projectAlias))),
				zap.String("codex_thread_id", strings.TrimSpace(outcome.codexThreadID)),
			)
			projectAlias := strings.ToLower(strings.TrimSpace(outcome.projectAlias))
			if projectAlias == "" {
				projectAlias = mcpCodexTopicAlias
			}
			if err := s.topicStore.Upsert(TopicBinding{
				ChatID:         msg.ChatID,
				FeishuThreadID: feishuThreadID,
				ProjectAlias:   projectAlias,
				CodexThreadID:  strings.TrimSpace(outcome.codexThreadID),
			}); err != nil {
				bindingLogger.Error("refresh topic binding failed", zap.Error(err))
				return err
			}
			bindingLogger.Info("refreshed topic binding")
		}
	}
	return nil
}

func (s *CommandService) startNewCodexSessionFromFollowup(ctx context.Context, prompt string, binding TopicBinding, previousThreadID string) (commandOutcome, error) {
	projectAlias := strings.ToLower(strings.TrimSpace(binding.ProjectAlias))
	startArgs := ensureCodexStartDefaults(map[string]any{
		"prompt": prompt,
	})
	if cwd, ok := s.cfg.ProjectAliasCWD[projectAlias]; ok && strings.TrimSpace(cwd) != "" {
		startArgs["cwd"] = cwd
	}
	output, err := s.mcp.CallTool(ctx, codexToolName, startArgs)
	if err != nil {
		return commandOutcome{}, err
	}
	resolvedThreadID := strings.TrimSpace(parseCodexThreadID(output))
	if resolvedThreadID == "" {
		resolvedThreadID = strings.TrimSpace(previousThreadID)
	}
	if projectAlias == "" {
		projectAlias = mcpCodexTopicAlias
	}
	return commandOutcome{
		text:          output,
		codexThreadID: resolvedThreadID,
		projectAlias:  projectAlias,
	}, nil
}

type commandOutcome struct {
	text          string
	codexThreadID string
	tokenUsage    string
	projectAlias  string
}

func (s *CommandService) executeWithHeartbeat(ctx context.Context, msg IncomingMessage, fn func(context.Context) (commandOutcome, error)) (commandOutcome, error) {
	startedAt := time.Now()
	msgLogger := s.messageLogger(msg)
	msgLogger.Info("command execution started")
	if s.sender != nil && strings.TrimSpace(msg.MessageID) != "" {
		err := s.sender.AddReaction(ctx, OutgoingReaction{
			MessageID: msg.MessageID,
			EmojiType: s.cfg.StartProcessingReaction,
		})
		if err != nil {
			msgLogger.Warn("failed to add start reaction", zap.Error(err))
		}
	}

	if s.cfg.Heartbeat <= 0 {
		outcome, runErr := fn(ctx)
		s.logCommandExecutionResult(msg, startedAt, outcome, runErr)
		return outcome, runErr
	}

	type result struct {
		outcome commandOutcome
		err     error
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	resultCh := make(chan result, 1)
	go func() {
		outcome, runErr := fn(workCtx)
		resultCh <- result{outcome: outcome, err: runErr}
	}()

	ticker := time.NewTicker(s.cfg.Heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			msgLogger.Warn("command execution canceled by context", zap.Duration("elapsed", time.Since(startedAt).Round(time.Second)))
			return commandOutcome{}, ctx.Err()
		case res := <-resultCh:
			s.logCommandExecutionResult(msg, startedAt, res.outcome, res.err)
			return res.outcome, res.err
		case <-ticker.C:
			heartbeatText := formatProcessingHeartbeat(time.Since(startedAt))
			if _, heartbeatErr := s.send(ctx, msg, heartbeatText, renderModePlainText); heartbeatErr != nil {
				s.outgoingMessageLogger(msg, heartbeatText, renderModePlainText).Error(
					"send heartbeat failed",
					zap.Duration("elapsed", time.Since(startedAt).Round(time.Second)),
					zap.Error(heartbeatErr),
				)
				return commandOutcome{}, heartbeatErr
			}
		}
	}
}

func formatProcessingHeartbeat(elapsed time.Duration) string {
	elapsed = elapsed.Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	return fmt.Sprintf("Still processing (elapsed %s), please wait...", formatElapsedDuration(elapsed))
}

func formatElapsedDuration(elapsed time.Duration) string {
	totalSeconds := int(elapsed.Seconds())
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func (s *CommandService) send(ctx context.Context, msg IncomingMessage, text, renderMode string) (SendReceipt, error) {
	renderMode = normalizeRenderMode(renderMode)
	outgoingLogger := s.outgoingMessageLogger(msg, text, renderMode)
	outgoingLogger.Info("outgoing feishu response")
	receipt, err := s.sender.Send(ctx, OutgoingMessage{
		ChatID:           msg.ChatID,
		ReplyToMessageID: msg.MessageID,
		ThreadID:         msg.ThreadID,
		Text:             text,
		RenderMode:       renderMode,
	})
	if err != nil {
		outgoingLogger.Error("send feishu response failed", zap.Error(err))
		return SendReceipt{}, err
	}
	outgoingLogger.Info("feishu response sent", zap.String("response_thread_id", strings.TrimSpace(receipt.ThreadID)))
	return receipt, nil
}

func (s *CommandService) replyCommandFailure(ctx context.Context, msg IncomingMessage, prefix string, cause error) error {
	msgLogger := s.messageLogger(msg)
	text := strings.TrimSpace(prefix)
	if text == "" {
		text = "Execution failed"
	}
	if cause != nil {
		text = fmt.Sprintf("%s: %v", text, cause)
		msgLogger.Error("command execution failed", zap.String("failure_message", text), zap.Error(cause))
	} else {
		msgLogger.Error("command execution failed", zap.String("failure_message", text))
	}
	text = attachDiagnosticID(text, msg)
	_, err := s.send(ctx, msg, text, renderModePlainText)
	if err != nil {
		return err
	}
	return nil
}

func isGroupChat(chatType string) bool {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	return chatType == "group" || chatType == "topic_group"
}

func stripMentions(text string) string {
	text = mentionTagPattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func parseProjectCommand(input string) (string, string, bool) {
	matches := projectCommandPattern.FindStringSubmatch(strings.TrimSpace(input))
	if len(matches) != 3 {
		return "", "", false
	}
	alias := strings.ToLower(strings.TrimSpace(matches[1]))
	prompt := strings.TrimSpace(matches[2])
	if alias == "" || prompt == "" {
		return "", "", false
	}
	return alias, prompt, true
}

func parseMCPCallCommand(input string) (string, string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(input, "/mcp call "))
	if rest == "" {
		return "", "", false
	}
	spaceIndex := strings.IndexAny(rest, " \t\n")
	if spaceIndex <= 0 {
		return "", "", false
	}
	tool := strings.TrimSpace(rest[:spaceIndex])
	jsonText := strings.TrimSpace(rest[spaceIndex+1:])
	if tool == "" || jsonText == "" {
		return "", "", false
	}
	return tool, jsonText, true
}

func normalizeOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "Completed."
	}
	return output
}

func formatCodexOutput(output, codexThreadID, tokenUsage string, notices ...string) string {
	body := strings.TrimSpace(output)
	if content, extractedThreadID, ok := extractCodexStructuredPayload(output); ok {
		if strings.TrimSpace(content) != "" {
			body = content
		}
		if strings.TrimSpace(codexThreadID) == "" {
			codexThreadID = extractedThreadID
		}
	}
	body = normalizeOutput(body)
	for _, notice := range notices {
		body = appendTextNotice(body, notice)
	}
	tokenUsage = strings.TrimSpace(tokenUsage)
	codexThreadID = strings.TrimSpace(codexThreadID)
	footer := make([]string, 0, 2)
	if tokenUsage != "" {
		footer = append(footer, fmt.Sprintf("context_window: %s", tokenUsage))
	}
	if codexThreadID != "" {
		footer = append(footer, fmt.Sprintf("codex_thread_id: %s", codexThreadID))
	}
	if len(footer) > 0 {
		body = appendTextNotice(body, "Thread info:\n"+strings.Join(footer, "\n"))
	}
	return normalizeOutput(body)
}

type codexContextWindowUsage struct {
	UsedTokens int64
	MaxTokens  int64
}

func (s *CommandService) resolveCodexContextWindowUsage(ctx context.Context, codexThreadID string, logger *zap.Logger) string {
	codexThreadID = strings.TrimSpace(codexThreadID)
	if codexThreadID == "" {
		return ""
	}

	statusOutput, err := s.mcp.CallTool(ctx, codexStatusToolName, map[string]any{
		"threadId": codexThreadID,
	})
	if err != nil {
		if logger != nil {
			logger.Debug("skip codex context window footer because codex-status failed", zap.String("codex_thread_id", codexThreadID), zap.Error(err))
		}
		return ""
	}

	usage, ok := parseCodexContextWindowUsage(statusOutput)
	if !ok {
		if logger != nil {
			logger.Debug("skip codex context window footer because codex-status output is not parseable", zap.String("codex_thread_id", codexThreadID))
		}
		return ""
	}
	return formatContextWindowUsage(usage)
}

func parseCodexContextWindowUsage(output string) (codexContextWindowUsage, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return codexContextWindowUsage{}, false
	}

	if payload, ok := decodeJSONObject(trimmed); ok {
		if usage, found := findCodexContextWindowUsage(payload); found {
			return usage, true
		}
	}

	if payload, ok := extractTrailingJSONObject(trimmed); ok {
		if usage, found := findCodexContextWindowUsage(payload); found {
			return usage, true
		}
	}

	matches := tokenUsagePattern.FindStringSubmatch(trimmed)
	if len(matches) != 3 {
		return codexContextWindowUsage{}, false
	}
	usedTokens, okUsed := parseTokenCount(matches[1])
	maxTokens, okMax := parseTokenCount(matches[2])
	if !okUsed || !okMax || maxTokens <= 0 {
		return codexContextWindowUsage{}, false
	}
	return codexContextWindowUsage{UsedTokens: usedTokens, MaxTokens: maxTokens}, true
}

func decodeJSONObject(text string) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractTrailingJSONObject(text string) (map[string]any, bool) {
	start := strings.LastIndex(text, "\n{")
	if start >= 0 {
		start++
	} else if strings.HasPrefix(text, "{") {
		start = 0
	} else {
		return nil, false
	}
	return decodeJSONObject(strings.TrimSpace(text[start:]))
}

func findCodexContextWindowUsage(value any) (codexContextWindowUsage, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if usage, ok := codexContextWindowUsageFromMap(typed); ok {
			return usage, true
		}
		for _, key := range []string{
			"contextWindow",
			"context_window",
			"contextWindowUsage",
			"context_window_usage",
			"usage",
			"tokenUsage",
			"token_usage",
		} {
			nested, exists := typed[key]
			if !exists {
				continue
			}
			if usage, ok := findCodexContextWindowUsage(nested); ok {
				return usage, true
			}
		}
		for _, nested := range typed {
			if usage, ok := findCodexContextWindowUsage(nested); ok {
				return usage, true
			}
		}
	case []any:
		for _, nested := range typed {
			if usage, ok := findCodexContextWindowUsage(nested); ok {
				return usage, true
			}
		}
	}
	return codexContextWindowUsage{}, false
}

func codexContextWindowUsageFromMap(payload map[string]any) (codexContextWindowUsage, bool) {
	usedTokens, okUsed := numericMapValue(payload,
		"usedTokens",
		"used_tokens",
		"tokensUsed",
		"tokens_used",
		"used",
	)
	maxTokens, okMax := numericMapValue(payload,
		"maxTokens",
		"max_tokens",
		"tokenLimit",
		"token_limit",
		"contextWindowTokens",
		"context_window_tokens",
		"max",
		"total",
	)
	if !okUsed || !okMax || maxTokens <= 0 {
		return codexContextWindowUsage{}, false
	}
	return codexContextWindowUsage{
		UsedTokens: usedTokens,
		MaxTokens:  maxTokens,
	}, true
}

func numericMapValue(payload map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		number, valid := numericValue(value)
		if !valid {
			continue
		}
		return number, true
	}
	return 0, false
}

func numericValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case float32:
		return int64(math.Round(float64(typed))), true
	case float64:
		return int64(math.Round(typed)), true
	case json.Number:
		if intValue, err := typed.Int64(); err == nil {
			return intValue, true
		}
		floatValue, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return int64(math.Round(floatValue)), true
	case string:
		return parseTokenCount(typed)
	default:
		return 0, false
	}
}

func parseTokenCount(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	cleaned := strings.ReplaceAll(raw, ",", "")
	cleaned = strings.ReplaceAll(cleaned, "_", "")
	if cleaned == "" {
		return 0, false
	}
	number, err := strconv.ParseInt(cleaned, 10, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func formatContextWindowUsage(usage codexContextWindowUsage) string {
	usedTokens := usage.UsedTokens
	maxTokens := usage.MaxTokens
	if maxTokens <= 0 {
		return ""
	}
	if usedTokens < 0 {
		usedTokens = 0
	}
	if usedTokens > maxTokens {
		usedTokens = maxTokens
	}
	leftRatio := float64(maxTokens-usedTokens) / float64(maxTokens)
	leftPercent := int(math.Round(leftRatio * 100))
	if leftPercent < 0 {
		leftPercent = 0
	}
	if leftPercent > 100 {
		leftPercent = 100
	}
	return fmt.Sprintf("%s / %s tokens used (%d%% left)", formatTokenCountCompact(usedTokens), formatTokenCountCompact(maxTokens), leftPercent)
}

func formatTokenCountCompact(tokens int64) string {
	if tokens < 0 {
		tokens = 0
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	return fmt.Sprintf("%dK", int(math.Round(float64(tokens)/1000)))
}

func extractCodexStructuredPayload(output string) (content string, threadID string, ok bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", "", false
	}

	start := strings.LastIndex(trimmed, "\n{")
	if start >= 0 {
		start++
	} else if strings.HasPrefix(trimmed, "{") {
		start = 0
	} else {
		return "", "", false
	}

	payloadRaw := strings.TrimSpace(trimmed[start:])
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return "", "", false
	}

	content = strings.TrimSpace(stringMapValue(payload, "content"))
	threadID = strings.TrimSpace(stringMapValue(payload, "threadId", "thread_id"))
	if content == "" && threadID == "" {
		return "", "", false
	}
	if content == "" {
		content = strings.TrimSpace(trimmed[:start])
	}
	return content, threadID, true
}

func stringMapValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(string); ok {
			typed = strings.TrimSpace(typed)
			if typed != "" {
				return typed
			}
		}
	}
	return ""
}

func appendTextNotice(text, notice string) string {
	text = strings.TrimSpace(text)
	notice = strings.TrimSpace(notice)
	if notice == "" {
		return text
	}
	if text == "" {
		return notice
	}
	return text + "\n\n" + notice
}

func isCodexReplySessionNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, `tool "codex-reply"`) &&
		strings.Contains(lowered, "session not found for thread_id")
}

func (s *CommandService) formatSessionResetNotice(msg IncomingMessage, binding TopicBinding) string {
	projectAlias := strings.TrimSpace(binding.ProjectAlias)
	if projectAlias == "" {
		projectAlias = mcpCodexTopicAlias
	}
	cwd := ""
	if resolvedCWD, ok := s.cfg.ProjectAliasCWD[strings.ToLower(projectAlias)]; ok {
		cwd = strings.TrimSpace(resolvedCWD)
	}
	if cwd == "" {
		cwd = "(not set)"
	}
	previousThreadID := strings.TrimSpace(binding.CodexThreadID)
	if previousThreadID == "" {
		previousThreadID = "(empty)"
	}
	feishuTopicID := chooseThreadID(msg.ThreadID, binding.FeishuThreadID)
	if feishuTopicID == "" {
		feishuTopicID = "(empty)"
	}
	return fmt.Sprintf(
		"Detected expired Codex session (automatically closed after %s). Starting a new session from this message and no longer reusing previous session context.\nEnvironment:\n- project_alias: %s\n- previous_codex_thread_id: %s\n- cwd: %s\n- feishu_topic_id: %s\n- chat_id: %s",
		formatElapsedDuration(codexSessionTimeout),
		projectAlias,
		previousThreadID,
		cwd,
		feishuTopicID,
		strings.TrimSpace(msg.ChatID),
	)
}

func chooseThreadID(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (s *CommandService) recordCodexThreadID(msg IncomingMessage, finalReceipt SendReceipt, output string) {
	chatID := strings.TrimSpace(msg.ChatID)
	feishuThreadID := chooseThreadID(msg.ThreadID, finalReceipt.ThreadID)
	codexThreadID := strings.TrimSpace(parseCodexThreadID(output))
	if chatID == "" || feishuThreadID == "" || codexThreadID == "" {
		return
	}

	if s.topicStore == nil {
		return
	}

	projectAlias := mcpCodexTopicAlias
	binding, ok := s.topicStore.Get(chatID, feishuThreadID)
	if ok && strings.TrimSpace(binding.ProjectAlias) != "" {
		projectAlias = strings.TrimSpace(binding.ProjectAlias)
	}
	bindingLogger := s.logger.With(
		zap.String("chat_id", chatID),
		zap.String("thread_id", feishuThreadID),
		zap.String("topic_id", feishuThreadID),
		zap.String("project_alias", projectAlias),
		zap.String("codex_thread_id", codexThreadID),
	)
	if err := s.topicStore.Upsert(TopicBinding{
		ChatID:         chatID,
		FeishuThreadID: feishuThreadID,
		ProjectAlias:   projectAlias,
		CodexThreadID:  codexThreadID,
	}); err != nil {
		bindingLogger.Warn("persist mcp codex topic binding failed", zap.Error(err))
		return
	}
	bindingLogger.Info("persisted mcp codex topic binding")
}

func parseCodexThreadID(output string) string {
	matches := codexThreadIDPattern.FindStringSubmatch(output)
	if len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	_, threadID, ok := extractCodexStructuredPayload(output)
	if !ok {
		return ""
	}
	return strings.TrimSpace(threadID)
}

func stringArg(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func sanitizeCodexStartArgs(args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	delete(args, "threadId")
	delete(args, "thread_id")
	delete(args, "conversationId")
	delete(args, "conversation_id")
	return ensureCodexStartDefaults(args)
}

func ensureCodexStartDefaults(args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	if strings.TrimSpace(stringArg(args, "model")) == "" {
		args["model"] = defaultCodexModel
	}
	if strings.TrimSpace(stringArg(args, "sandbox")) == "" {
		args["sandbox"] = defaultCodexSandbox
	}
	if strings.TrimSpace(stringArg(args, "approval-policy")) == "" {
		args["approval-policy"] = defaultCodexApprovalPolicy
	}

	config, ok := args["config"].(map[string]any)
	if !ok || config == nil {
		config = map[string]any{}
	}
	if strings.TrimSpace(stringArg(config, "model_reasoning_effort")) == "" {
		config["model_reasoning_effort"] = defaultCodexReasoningEffort
	}
	args["config"] = config
	return args
}

func (s *CommandService) bindingSupportsProjectFollowup(binding TopicBinding) bool {
	return strings.TrimSpace(binding.CodexThreadID) != ""
}

func (s *CommandService) messageLogger(msg IncomingMessage) *zap.Logger {
	return s.logger.With(baseMessageLogFields(msg)...)
}

func (s *CommandService) outgoingMessageLogger(msg IncomingMessage, text, renderMode string) *zap.Logger {
	return s.logger.With(outgoingMessageLogFields(msg, text, renderMode)...)
}

func baseMessageLogFields(msg IncomingMessage) []zap.Field {
	mentionedIDs := make([]string, 0, len(msg.MentionedIDs))
	for _, id := range msg.MentionedIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		mentionedIDs = append(mentionedIDs, id)
	}
	sort.Strings(mentionedIDs)

	threadID := strings.TrimSpace(msg.ThreadID)
	return []zap.Field{
		zap.String("chat_id", strings.TrimSpace(msg.ChatID)),
		zap.String("chat_type", strings.TrimSpace(msg.ChatType)),
		zap.String("message_id", strings.TrimSpace(msg.MessageID)),
		zap.String("thread_id", threadID),
		zap.String("topic_id", threadID),
		zap.String("request_id", strings.TrimSpace(msg.RequestID)),
		zap.String("correlation_id", strings.TrimSpace(msg.CorrelationID)),
		zap.Strings("mentioned_ids", mentionedIDs),
	}
}

func outgoingMessageLogFields(msg IncomingMessage, text, renderMode string) []zap.Field {
	fields := make([]zap.Field, 0, 9)
	fields = append(fields, baseMessageLogFields(msg)...)
	fields = append(
		fields,
		zap.String("reply_to_message_id", strings.TrimSpace(msg.MessageID)),
		zap.String("render_mode", normalizeRenderMode(renderMode)),
		zap.String("text", strings.TrimSpace(text)),
		zap.Int("text_runes", utf8.RuneCountInString(strings.TrimSpace(text))),
	)
	return fields
}

func normalizeRenderMode(renderMode string) string {
	renderMode = strings.TrimSpace(strings.ToLower(renderMode))
	if renderMode == renderModeCodexMarkdown {
		return renderModeCodexMarkdown
	}
	return renderModePlainText
}

func (s *CommandService) logCommandExecutionResult(msg IncomingMessage, startedAt time.Time, outcome commandOutcome, err error) {
	logger := s.messageLogger(msg).With(zap.Duration("elapsed", time.Since(startedAt).Round(time.Second)))
	if strings.TrimSpace(outcome.projectAlias) != "" {
		logger = logger.With(zap.String("project_alias", strings.TrimSpace(outcome.projectAlias)))
	}
	if strings.TrimSpace(outcome.codexThreadID) != "" {
		logger = logger.With(zap.String("codex_thread_id", strings.TrimSpace(outcome.codexThreadID)))
	}
	if err != nil {
		logger.Error("command execution finished with error", zap.Error(err))
		return
	}
	logger.Info("command execution finished")
}

// EnsureTraceIDs guarantees request and correlation IDs exist for log correlation.
func EnsureTraceIDs(msg IncomingMessage) IncomingMessage {
	msg.RequestID = normalizeRequestID(msg.RequestID, msg.MessageID)
	msg.CorrelationID = normalizeCorrelationID(msg.CorrelationID, msg.ChatID, msg.ThreadID, msg.MessageID, msg.RequestID)
	return msg
}

func normalizeRequestID(existing, messageID string) string {
	existing = strings.TrimSpace(existing)
	if existing != "" {
		return existing
	}
	messageID = strings.TrimSpace(messageID)
	if messageID != "" {
		return "req_" + messageID
	}
	return "req_" + randomIDSuffix()
}

func normalizeCorrelationID(existing, chatID, threadID, messageID, requestID string) string {
	existing = strings.TrimSpace(existing)
	if existing != "" {
		return existing
	}
	chatID = strings.TrimSpace(chatID)
	threadID = strings.TrimSpace(threadID)
	messageID = strings.TrimSpace(messageID)
	requestID = strings.TrimSpace(requestID)
	if chatID != "" && threadID != "" {
		return "corr_" + chatID + "_" + threadID
	}
	if chatID != "" && messageID != "" {
		return "corr_" + chatID + "_" + messageID
	}
	if requestID != "" {
		return "corr_" + requestID
	}
	return "corr_" + randomIDSuffix()
}

func randomIDSuffix() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func attachDiagnosticID(text string, msg IncomingMessage) string {
	text = strings.TrimSpace(text)
	requestID := strings.TrimSpace(msg.RequestID)
	if requestID == "" {
		return text
	}
	return fmt.Sprintf("%s\nDiagnostic ID: %s", text, requestID)
}
