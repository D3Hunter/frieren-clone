package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
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

// CodexGateway describes Codex thread start/reply operations needed by CommandService.
type CodexGateway interface {
	// Start runs a new Codex thread in cwd for prompt and returns thread ID plus output text.
	Start(ctx context.Context, cwd, prompt string) (threadID string, output string, err error)
	// Reply runs a follow-up prompt in an existing Codex thread.
	Reply(ctx context.Context, cwd, threadID, prompt string) (output string, err error)
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
	Codex      CodexGateway
	Sender     MessageSender
	TopicStore TopicStore
	Logger     *zap.Logger
	Config     CommandServiceConfig
}

// CommandService parses incoming messages and routes them to MCP/Codex workflows.
type CommandService struct {
	mcp        MCPGateway
	codex      CodexGateway
	sender     MessageSender
	topicStore TopicStore
	cfg        CommandServiceConfig
	logger     *zap.Logger

	mcpCodexTopicThreads map[string]string
	mcpCodexTopicMu      sync.RWMutex
}

var mentionTagPattern = regexp.MustCompile(`(?s)<at\b[^>]*>.*?</at>`)
var projectCommandPattern = regexp.MustCompile(`^/([a-zA-Z0-9_-]+)\s+(.+)$`)
var codexThreadIDPattern = regexp.MustCompile(`(?i)"thread(?:_|)id"\s*:\s*"([^"]+)"`)

const (
	processingStartReactionType = "OnIt"
	codexToolName               = "codex"
	mcpCodexTopicAlias          = "__mcp_codex__"
	defaultHelpMessage          = "可用命令：\n/help\n/mcp tools\n/mcp schema <tool>\n/mcp call <tool> <json>\n/<project> <prompt>\n\n提示：/mcp call codex 每次都会新建 Codex 线程。"
	codexPromptHelpMessage      = `用法：/mcp call codex {"prompt":"<你的问题>"}`
	// Intentionally keep /mcp call codex as "start new thread" so users can open
	// multiple independent Codex threads inside one Feishu topic when needed.
	codexNewThreadNotice     = "提示：按设计，/mcp call codex 每次都会新建 Codex 线程，不会复用当前话题绑定。"
	groupMentionHelpMessage  = "群聊里请先 @机器人 再发送斜杠命令，例如：@机器人 /help"
	unknownProjectHelpPrefix = "未知项目别名"
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
		codex:      deps.Codex,
		sender:     deps.Sender,
		topicStore: deps.TopicStore,
		cfg:        cfg,
		logger:     logger,

		mcpCodexTopicThreads: map[string]string{},
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
		_, err := s.send(ctx, msg, "请输入命令，使用 /help 查看帮助。")
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
			_, err := s.send(ctx, msg, groupMentionHelpMessage)
			return err
		}
		return s.handleSlashCommand(ctx, msg, cleanText)
	}

	if hasBinding {
		return s.handleTopicFollowup(ctx, msg, cleanText, binding)
	}

	msgLogger.Info("plain text without topic binding; returning help")
	_, err := s.send(ctx, msg, "请使用 /help 查看命令格式。")
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
		_, err := s.send(ctx, msg, defaultHelpMessage)
		return err
	case cleanText == "/mcp tools":
		return s.handleMCPTools(ctx, msg)
	case strings.HasPrefix(cleanText, "/mcp schema "):
		tool := strings.TrimSpace(strings.TrimPrefix(cleanText, "/mcp schema "))
		if tool == "" {
			_, err := s.send(ctx, msg, "用法：/mcp schema <tool>")
			return err
		}
		return s.handleMCPSchema(ctx, msg, tool)
	case strings.HasPrefix(cleanText, "/mcp call "):
		tool, argsRaw, ok := parseMCPCallCommand(cleanText)
		if !ok {
			_, err := s.send(ctx, msg, "用法：/mcp call <tool> <json>")
			return err
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
			_, sendErr := s.send(ctx, msg, attachDiagnosticID(fmt.Sprintf("JSON 解析失败：%v", err), msg))
			if sendErr != nil {
				return sendErr
			}
			return nil
		}
		if args == nil {
			args = map[string]any{}
		}
		if strings.EqualFold(tool, codexToolName) && strings.TrimSpace(stringArg(args, "prompt")) == "" {
			_, err := s.send(ctx, msg, codexPromptHelpMessage)
			return err
		}
		return s.handleMCPCall(ctx, msg, tool, args)
	default:
		alias, prompt, ok := parseProjectCommand(cleanText)
		if !ok {
			_, err := s.send(ctx, msg, defaultHelpMessage)
			return err
		}
		msgLogger.Info(
			"handling project command",
			zap.String("project_alias", alias),
			zap.String("prompt", prompt),
		)
		cwd, ok := s.cfg.ProjectAliasCWD[alias]
		if !ok || strings.TrimSpace(cwd) == "" {
			_, err := s.send(ctx, msg, fmt.Sprintf("%s：%s", unknownProjectHelpPrefix, alias))
			return err
		}
		outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
			threadID, output, runErr := s.codex.Start(runCtx, cwd, prompt)
			if runErr != nil {
				return commandOutcome{}, runErr
			}
			return commandOutcome{text: output, codexThreadID: threadID, projectAlias: alias}, nil
		})
		if err != nil {
			return s.replyCommandFailure(ctx, msg, "执行失败", err)
		}
		finalReceipt, err := s.send(ctx, msg, normalizeOutput(outcome.text))
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
			return commandOutcome{text: "当前没有可用 MCP 工具。"}, nil
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		lines := make([]string, 0, len(tools)+1)
		lines = append(lines, "可用 MCP 工具：")
		for _, tool := range tools {
			line := "- " + tool.Name
			if strings.TrimSpace(tool.Description) != "" {
				line += "：" + tool.Description
			}
			lines = append(lines, line)
		}
		return commandOutcome{text: strings.Join(lines, "\n")}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "获取工具失败", err)
	}
	_, err = s.send(ctx, msg, normalizeOutput(outcome.text))
	return err
}

func (s *CommandService) handleMCPSchema(ctx context.Context, msg IncomingMessage, tool string) error {
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		schema, runErr := s.mcp.GetToolSchema(runCtx, tool)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		return commandOutcome{text: fmt.Sprintf("%s 的参数 schema：\n%s", tool, schema)}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "获取 schema 失败", err)
	}
	_, err = s.send(ctx, msg, normalizeOutput(outcome.text))
	return err
}

func (s *CommandService) handleMCPCall(ctx context.Context, msg IncomingMessage, tool string, args map[string]any) error {
	tool = strings.TrimSpace(tool)
	msgLogger := s.messageLogger(msg)
	msgLogger.Info("handling mcp call", zap.String("tool", tool), zap.Any("args", args))
	if strings.EqualFold(tool, codexToolName) {
		s.injectCodexThreadID(msg.ChatID, msg.ThreadID, args)
	}
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		result, runErr := s.mcp.CallTool(runCtx, tool, args)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		return commandOutcome{text: result}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "调用工具失败", err)
	}
	responseText := normalizeOutput(outcome.text)
	if strings.EqualFold(tool, codexToolName) {
		responseText = appendTextNotice(responseText, codexNewThreadNotice)
	}
	finalReceipt, err := s.send(ctx, msg, responseText)
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
	alias := strings.ToLower(strings.TrimSpace(binding.ProjectAlias))
	cwd := strings.TrimSpace(s.cfg.ProjectAliasCWD[alias])
	if cwd == "" {
		_, err := s.send(ctx, msg, fmt.Sprintf("%s：%s", unknownProjectHelpPrefix, alias))
		return err
	}
	outcome, err := s.executeWithHeartbeat(ctx, msg, func(runCtx context.Context) (commandOutcome, error) {
		output, runErr := s.codex.Reply(runCtx, cwd, binding.CodexThreadID, cleanText)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		return commandOutcome{text: output, codexThreadID: binding.CodexThreadID, projectAlias: alias}, nil
	})
	if err != nil {
		return s.replyCommandFailure(ctx, msg, "执行失败", err)
	}
	finalReceipt, err := s.send(ctx, msg, normalizeOutput(outcome.text))
	if err != nil {
		return err
	}
	if s.topicStore != nil {
		feishuThreadID := chooseThreadID(msg.ThreadID, finalReceipt.ThreadID)
		if feishuThreadID != "" {
			bindingLogger := msgLogger.With(
				zap.String("topic_id", feishuThreadID),
				zap.String("project_alias", alias),
				zap.String("codex_thread_id", strings.TrimSpace(binding.CodexThreadID)),
			)
			if err := s.topicStore.Upsert(TopicBinding{
				ChatID:         msg.ChatID,
				FeishuThreadID: feishuThreadID,
				ProjectAlias:   alias,
				CodexThreadID:  binding.CodexThreadID,
			}); err != nil {
				bindingLogger.Error("refresh topic binding failed", zap.Error(err))
				return err
			}
			bindingLogger.Info("refreshed topic binding")
		}
	}
	return nil
}

type commandOutcome struct {
	text          string
	codexThreadID string
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
			if _, heartbeatErr := s.send(ctx, msg, heartbeatText); heartbeatErr != nil {
				s.outgoingMessageLogger(msg, heartbeatText).Error(
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
	return fmt.Sprintf("仍在处理中（已运行 %s），请稍候…", formatElapsedDuration(elapsed))
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
		return fmt.Sprintf("%d小时%02d分%02d秒", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d分%02d秒", minutes, seconds)
}

func (s *CommandService) send(ctx context.Context, msg IncomingMessage, text string) (SendReceipt, error) {
	outgoingLogger := s.outgoingMessageLogger(msg, text)
	outgoingLogger.Info("outgoing feishu response")
	receipt, err := s.sender.Send(ctx, OutgoingMessage{
		ChatID:           msg.ChatID,
		ReplyToMessageID: msg.MessageID,
		ThreadID:         msg.ThreadID,
		Text:             text,
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
		text = "执行失败"
	}
	if cause != nil {
		text = fmt.Sprintf("%s：%v", text, cause)
		msgLogger.Error("command execution failed", zap.String("failure_message", text), zap.Error(cause))
	} else {
		msgLogger.Error("command execution failed", zap.String("failure_message", text))
	}
	text = attachDiagnosticID(text, msg)
	_, err := s.send(ctx, msg, text)
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
		return "执行完成。"
	}
	return output
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

func chooseThreadID(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (s *CommandService) injectCodexThreadID(chatID, feishuThreadID string, args map[string]any) {
	if args == nil {
		return
	}
	if strings.TrimSpace(stringArg(args, "threadId")) != "" {
		return
	}

	existing := s.lookupCodexThreadID(chatID, feishuThreadID)
	if existing == "" {
		return
	}
	s.logger.Info(
		"injecting codex thread id into mcp call args",
		zap.String("chat_id", strings.TrimSpace(chatID)),
		zap.String("thread_id", strings.TrimSpace(feishuThreadID)),
		zap.String("topic_id", strings.TrimSpace(feishuThreadID)),
		zap.String("codex_thread_id", existing),
	)
	args["threadId"] = existing
}

func (s *CommandService) lookupCodexThreadID(chatID, feishuThreadID string) string {
	chatID = strings.TrimSpace(chatID)
	feishuThreadID = strings.TrimSpace(feishuThreadID)
	if chatID == "" || feishuThreadID == "" {
		return ""
	}

	s.mcpCodexTopicMu.RLock()
	threadID := strings.TrimSpace(s.mcpCodexTopicThreads[topicThreadKey(chatID, feishuThreadID)])
	s.mcpCodexTopicMu.RUnlock()
	if threadID != "" {
		return threadID
	}

	if s.topicStore == nil {
		return ""
	}
	binding, ok := s.topicStore.Get(chatID, feishuThreadID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(binding.CodexThreadID)
}

func (s *CommandService) recordCodexThreadID(msg IncomingMessage, finalReceipt SendReceipt, output string) {
	chatID := strings.TrimSpace(msg.ChatID)
	feishuThreadID := chooseThreadID(msg.ThreadID, finalReceipt.ThreadID)
	codexThreadID := strings.TrimSpace(parseCodexThreadID(output))
	if chatID == "" || feishuThreadID == "" || codexThreadID == "" {
		return
	}

	s.mcpCodexTopicMu.Lock()
	s.mcpCodexTopicThreads[topicThreadKey(chatID, feishuThreadID)] = codexThreadID
	s.mcpCodexTopicMu.Unlock()

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
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func topicThreadKey(chatID, feishuThreadID string) string {
	return strings.TrimSpace(chatID) + "::" + strings.TrimSpace(feishuThreadID)
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

func (s *CommandService) bindingSupportsProjectFollowup(binding TopicBinding) bool {
	alias := strings.ToLower(strings.TrimSpace(binding.ProjectAlias))
	if alias == "" || alias == mcpCodexTopicAlias {
		return false
	}
	cwd := strings.TrimSpace(s.cfg.ProjectAliasCWD[alias])
	return cwd != ""
}

func (s *CommandService) messageLogger(msg IncomingMessage) *zap.Logger {
	return s.logger.With(baseMessageLogFields(msg)...)
}

func (s *CommandService) outgoingMessageLogger(msg IncomingMessage, text string) *zap.Logger {
	return s.logger.With(outgoingMessageLogFields(msg, text)...)
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

func outgoingMessageLogFields(msg IncomingMessage, text string) []zap.Field {
	fields := make([]zap.Field, 0, 9)
	fields = append(fields, baseMessageLogFields(msg)...)
	fields = append(
		fields,
		zap.String("reply_to_message_id", strings.TrimSpace(msg.MessageID)),
		zap.String("text", strings.TrimSpace(text)),
		zap.Int("text_runes", utf8.RuneCountInString(strings.TrimSpace(text))),
	)
	return fields
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
	return fmt.Sprintf("%s\n诊断ID：%s", text, requestID)
}
