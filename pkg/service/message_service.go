package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type ToolInfo struct {
	Name        string
	Description string
}

type MCPGateway interface {
	ListTools(ctx context.Context) ([]ToolInfo, error)
	GetToolSchema(ctx context.Context, tool string) (string, error)
	CallTool(ctx context.Context, tool string, args map[string]any) (string, error)
}

type CodexGateway interface {
	Start(ctx context.Context, cwd, prompt string) (threadID string, output string, err error)
	Reply(ctx context.Context, cwd, threadID, prompt string) (output string, err error)
}

type TopicBinding struct {
	ChatID         string
	FeishuThreadID string
	ProjectAlias   string
	CodexThreadID  string
}

type TopicStore interface {
	Get(chatID, feishuThreadID string) (TopicBinding, bool)
	Upsert(binding TopicBinding) error
}

type OutgoingMessage struct {
	ChatID           string
	ReplyToMessageID string
	ThreadID         string
	Text             string
}

type OutgoingReaction struct {
	MessageID string
	EmojiType string
}

type SendReceipt struct {
	ThreadID string
}

type MessageSender interface {
	Send(ctx context.Context, msg OutgoingMessage) (SendReceipt, error)
	AddReaction(ctx context.Context, reaction OutgoingReaction) error
}

type IncomingMessage struct {
	ChatID       string
	MessageID    string
	ThreadID     string
	ChatType     string
	RawText      string
	MentionedIDs []string
}

type CommandServiceConfig struct {
	BotOpenID               string
	Heartbeat               time.Duration
	StartProcessingReaction string
	ProjectAliasCWD         map[string]string
}

type CommandServiceDeps struct {
	MCP        MCPGateway
	Codex      CodexGateway
	Sender     MessageSender
	TopicStore TopicStore
	Config     CommandServiceConfig
}

type CommandService struct {
	mcp        MCPGateway
	codex      CodexGateway
	sender     MessageSender
	topicStore TopicStore
	cfg        CommandServiceConfig
}

var mentionTagPattern = regexp.MustCompile(`(?s)<at\b[^>]*>.*?</at>`)
var projectCommandPattern = regexp.MustCompile(`^/([a-zA-Z0-9_-]+)\s+(.+)$`)

const (
	processingStartReactionType = "OnIt"
	defaultHelpMessage          = "可用命令：\n/help\n/mcp tools\n/mcp schema <tool>\n/mcp call <tool> <json>\n/<project> <prompt>"
	groupMentionHelpMessage     = "群聊里请先 @机器人 再发送斜杠命令，例如：@机器人 /help"
	unknownProjectHelpPrefix    = "未知项目别名"
)

func NewCommandService(deps CommandServiceDeps) *CommandService {
	cfg := deps.Config
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
	}
}

func (s *CommandService) HandleIncomingMessage(ctx context.Context, msg IncomingMessage) error {
	msg.ChatID = strings.TrimSpace(msg.ChatID)
	msg.MessageID = strings.TrimSpace(msg.MessageID)
	msg.ThreadID = strings.TrimSpace(msg.ThreadID)
	if msg.ChatID == "" {
		return fmt.Errorf("chat id is required")
	}
	if msg.MessageID == "" {
		return fmt.Errorf("message id is required")
	}

	text := strings.TrimSpace(msg.RawText)
	if text == "" {
		_, err := s.send(ctx, msg, "请输入命令，使用 /help 查看帮助。")
		return err
	}
	cleanText := stripMentions(text)

	binding, hasBinding := TopicBinding{}, false
	if msg.ThreadID != "" && s.topicStore != nil {
		binding, hasBinding = s.topicStore.Get(msg.ChatID, msg.ThreadID)
	}

	if strings.HasPrefix(cleanText, "/") {
		if s.requiresMention(cleanText, msg) {
			_, err := s.send(ctx, msg, groupMentionHelpMessage)
			return err
		}
		return s.handleSlashCommand(ctx, msg, cleanText)
	}

	if hasBinding {
		return s.handleTopicFollowup(ctx, msg, cleanText, binding)
	}

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
			_, sendErr := s.send(ctx, msg, fmt.Sprintf("JSON 解析失败：%v", err))
			if sendErr != nil {
				return sendErr
			}
			return nil
		}
		if args == nil {
			args = map[string]any{}
		}
		return s.handleMCPCall(ctx, msg, tool, args)
	default:
		alias, prompt, ok := parseProjectCommand(cleanText)
		if !ok {
			_, err := s.send(ctx, msg, defaultHelpMessage)
			return err
		}
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
				if err := s.topicStore.Upsert(TopicBinding{
					ChatID:         msg.ChatID,
					FeishuThreadID: feishuThreadID,
					ProjectAlias:   alias,
					CodexThreadID:  outcome.codexThreadID,
				}); err != nil {
					return err
				}
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
	_, err = s.send(ctx, msg, normalizeOutput(outcome.text))
	return err
}

func (s *CommandService) handleTopicFollowup(ctx context.Context, msg IncomingMessage, cleanText string, binding TopicBinding) error {
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
			if err := s.topicStore.Upsert(TopicBinding{
				ChatID:         msg.ChatID,
				FeishuThreadID: feishuThreadID,
				ProjectAlias:   alias,
				CodexThreadID:  binding.CodexThreadID,
			}); err != nil {
				return err
			}
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
	if s.sender != nil && strings.TrimSpace(msg.MessageID) != "" {
		_ = s.sender.AddReaction(ctx, OutgoingReaction{
			MessageID: msg.MessageID,
			EmojiType: s.cfg.StartProcessingReaction,
		})
	}

	if s.cfg.Heartbeat <= 0 {
		outcome, runErr := fn(ctx)
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
			return commandOutcome{}, ctx.Err()
		case res := <-resultCh:
			return res.outcome, res.err
		case <-ticker.C:
			heartbeatText := formatProcessingHeartbeat(time.Since(startedAt))
			if _, heartbeatErr := s.send(ctx, msg, heartbeatText); heartbeatErr != nil {
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
	receipt, err := s.sender.Send(ctx, OutgoingMessage{
		ChatID:           msg.ChatID,
		ReplyToMessageID: msg.MessageID,
		ThreadID:         msg.ThreadID,
		Text:             text,
	})
	if err != nil {
		return SendReceipt{}, err
	}
	return receipt, nil
}

func (s *CommandService) replyCommandFailure(ctx context.Context, msg IncomingMessage, prefix string, cause error) error {
	text := strings.TrimSpace(prefix)
	if text == "" {
		text = "执行失败"
	}
	if cause != nil {
		text = fmt.Sprintf("%s：%v", text, cause)
	}
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

func chooseThreadID(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}
