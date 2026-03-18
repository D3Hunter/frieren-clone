package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type fakeMCPGateway struct {
	tools      []ToolInfo
	schemaText string
	callText   string
	callTexts  []string
	callErr    error
	callDelay  time.Duration
	calledWith struct {
		tool string
		args map[string]any
	}
	callHistory []struct {
		tool string
		args map[string]any
	}
}

func (f *fakeMCPGateway) ListTools(ctx context.Context) ([]ToolInfo, error) {
	return f.tools, nil
}

func (f *fakeMCPGateway) GetToolSchema(ctx context.Context, tool string) (string, error) {
	if f.schemaText == "" {
		return "{}", nil
	}
	return f.schemaText, nil
}

func (f *fakeMCPGateway) CallTool(ctx context.Context, tool string, args map[string]any) (string, error) {
	if f.callDelay > 0 {
		time.Sleep(f.callDelay)
	}
	f.calledWith.tool = tool
	f.calledWith.args = args
	clonedArgs := make(map[string]any, len(args))
	for key, value := range args {
		clonedArgs[key] = value
	}
	f.callHistory = append(f.callHistory, struct {
		tool string
		args map[string]any
	}{tool: tool, args: clonedArgs})
	if f.callErr != nil {
		return "", f.callErr
	}
	if len(f.callTexts) > 0 {
		next := f.callTexts[0]
		f.callTexts = f.callTexts[1:]
		return next, nil
	}
	if f.callText == "" {
		return "ok", nil
	}
	return f.callText, nil
}

func TestHandleIncomingMessage_MCPCallCodexAlwaysStartsNewThreadWithinTopic(t *testing.T) {
	mcp := &fakeMCPGateway{
		callText: "ok\n{\"threadId\":\"codex_t1\"}",
	}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	first := IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"pwd"}`,
		MentionedIDs: []string{"ou_bot"},
	}
	if err := svc.HandleIncomingMessage(context.Background(), first); err != nil {
		t.Fatalf("first HandleIncomingMessage error: %v", err)
	}

	second := IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_2",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"ls"}`,
		MentionedIDs: []string{"ou_bot"},
	}
	if err := svc.HandleIncomingMessage(context.Background(), second); err != nil {
		t.Fatalf("second HandleIncomingMessage error: %v", err)
	}

	if len(mcp.callHistory) != 2 {
		t.Fatalf("expected two codex calls, got %d", len(mcp.callHistory))
	}
	if _, ok := mcp.callHistory[0].args["threadId"]; ok {
		t.Fatalf("expected first call without injected threadId, got %#v", mcp.callHistory[0].args["threadId"])
	}
	if _, ok := mcp.callHistory[1].args["threadId"]; ok {
		t.Fatalf("expected second call without injected threadId, got %#v", mcp.callHistory[1].args["threadId"])
	}
}

func TestHandleIncomingMessage_MCPCallCodexPersistsTopicThreadForFollowupWithoutInjection(t *testing.T) {
	topicStore := newFakeTopicStore()
	firstMCP := &fakeMCPGateway{
		callText: "ok\n{\"threadId\":\"codex_t1\"}",
	}
	firstSvc := NewCommandService(CommandServiceDeps{
		MCP:        firstMCP,
		Sender:     &fakeMessageSender{},
		TopicStore: topicStore,
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	firstMsg := IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"pwd"}`,
		MentionedIDs: []string{"ou_bot"},
	}
	if err := firstSvc.HandleIncomingMessage(context.Background(), firstMsg); err != nil {
		t.Fatalf("first HandleIncomingMessage error: %v", err)
	}

	binding, ok := topicStore.Get("oc_chat", "omt_topic")
	if !ok {
		t.Fatal("expected persisted topic binding for mcp codex thread")
	}
	if binding.ProjectAlias != mcpCodexTopicAlias {
		t.Fatalf("expected mcp codex alias, got %q", binding.ProjectAlias)
	}
	if binding.CodexThreadID != "codex_t1" {
		t.Fatalf("expected codex_t1, got %q", binding.CodexThreadID)
	}

	secondMCP := &fakeMCPGateway{
		callText: "ok\n{\"threadId\":\"codex_t1\"}",
	}
	secondSvc := NewCommandService(CommandServiceDeps{
		MCP:        secondMCP,
		Sender:     &fakeMessageSender{},
		TopicStore: topicStore,
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	secondMsg := IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_2",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"ls"}`,
		MentionedIDs: []string{"ou_bot"},
	}
	if err := secondSvc.HandleIncomingMessage(context.Background(), secondMsg); err != nil {
		t.Fatalf("second HandleIncomingMessage error: %v", err)
	}

	if len(secondMCP.callHistory) != 1 {
		t.Fatalf("expected one call on second service, got %d", len(secondMCP.callHistory))
	}
	if _, ok := secondMCP.callHistory[0].args["threadId"]; ok {
		t.Fatalf("expected no injected threadId, got %#v", secondMCP.callHistory[0].args["threadId"])
	}
}

type fakeTopicStore struct {
	mu      sync.Mutex
	entries map[string]TopicBinding
}

func newFakeTopicStore() *fakeTopicStore {
	return &fakeTopicStore{entries: map[string]TopicBinding{}}
}

func topicKey(chatID, threadID string) string { return chatID + "::" + threadID }

func (s *fakeTopicStore) Get(chatID, feishuThreadID string) (TopicBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[topicKey(chatID, feishuThreadID)]
	return v, ok
}

func (s *fakeTopicStore) Upsert(binding TopicBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[topicKey(binding.ChatID, binding.FeishuThreadID)] = binding
	return nil
}

type fakeMessageSender struct {
	messages     []OutgoingMessage
	reactions    []OutgoingReaction
	fixedThread  string
	returnThread string
	err          error
}

func (s *fakeMessageSender) Send(ctx context.Context, msg OutgoingMessage) (SendReceipt, error) {
	s.messages = append(s.messages, msg)
	if s.err != nil {
		return SendReceipt{}, s.err
	}
	threadID := s.returnThread
	if threadID == "" {
		threadID = s.fixedThread
	}
	if threadID == "" {
		threadID = msg.ThreadID
	}
	if threadID == "" {
		threadID = "omt_generated"
	}
	return SendReceipt{ThreadID: threadID}, nil
}

func (s *fakeMessageSender) AddReaction(ctx context.Context, reaction OutgoingReaction) error {
	s.reactions = append(s.reactions, reaction)
	if s.err != nil {
		return s.err
	}
	return nil
}

func TestHandleIncomingMessage_HelpCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config: CommandServiceConfig{
			BotOpenID:       "ou_bot",
			Heartbeat:       time.Hour,
			ProjectAliasCWD: map[string]string{"tidb": "/tmp/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /help",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(sender.messages) == 0 {
		t.Fatal("expected help response")
	}
	if !strings.Contains(sender.messages[len(sender.messages)-1].Text, "/mcp tools") {
		t.Fatalf("unexpected help text: %q", sender.messages[len(sender.messages)-1].Text)
	}
}

func TestHandleIncomingMessage_HelpCommandMentionsMCPCallCodexStartsNewThread(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config: CommandServiceConfig{
			BotOpenID:       "ou_bot",
			Heartbeat:       time.Hour,
			ProjectAliasCWD: map[string]string{"tidb": "/tmp/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /help",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(sender.messages) == 0 {
		t.Fatal("expected help response")
	}
	helpText := sender.messages[len(sender.messages)-1].Text
	if !strings.Contains(helpText, "/mcp call codex") || !strings.Contains(helpText, "每次都会新建") {
		t.Fatalf("expected /help to mention /mcp call codex starts new thread, got %q", helpText)
	}
}

func TestHandleIncomingMessage_LogsIncomingAndOutgoingMessageDetails(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Logger:     logger,
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /help`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	var incomingLogged bool
	var outgoingLogged bool
	for _, entry := range observed.All() {
		fields := entry.ContextMap()
		switch entry.Message {
		case "incoming feishu message":
			incomingLogged = true
			if fields["chat_id"] != "oc_chat" {
				t.Fatalf("incoming log missing chat_id: %+v", fields)
			}
			if fields["message_id"] != "om_1" {
				t.Fatalf("incoming log missing message_id: %+v", fields)
			}
			if fields["thread_id"] != "omt_topic" {
				t.Fatalf("incoming log missing thread_id: %+v", fields)
			}
			if fields["topic_id"] != "omt_topic" {
				t.Fatalf("incoming log missing topic_id: %+v", fields)
			}
			if fields["request_id"] != "req_om_1" {
				t.Fatalf("incoming log missing request_id: %+v", fields)
			}
			if fields["correlation_id"] != "corr_oc_chat_omt_topic" {
				t.Fatalf("incoming log missing correlation_id: %+v", fields)
			}
		case "outgoing feishu response":
			outgoingLogged = true
			if fields["chat_id"] != "oc_chat" {
				t.Fatalf("outgoing log missing chat_id: %+v", fields)
			}
			if fields["reply_to_message_id"] != "om_1" {
				t.Fatalf("outgoing log missing reply_to_message_id: %+v", fields)
			}
			if fields["thread_id"] != "omt_topic" {
				t.Fatalf("outgoing log missing thread_id: %+v", fields)
			}
			if fields["topic_id"] != "omt_topic" {
				t.Fatalf("outgoing log missing topic_id: %+v", fields)
			}
			if fields["request_id"] != "req_om_1" {
				t.Fatalf("outgoing log missing request_id: %+v", fields)
			}
			if fields["correlation_id"] != "corr_oc_chat_omt_topic" {
				t.Fatalf("outgoing log missing correlation_id: %+v", fields)
			}
			if !strings.Contains(fields["text"].(string), "/mcp tools") {
				t.Fatalf("outgoing log missing response text: %+v", fields)
			}
		}
	}
	if !incomingLogged {
		t.Fatal("expected incoming message log")
	}
	if !outgoingLogged {
		t.Fatal("expected outgoing response log")
	}
}

func TestHandleIncomingMessage_GroupCommandRequiresMention(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:    "oc_chat",
		ChatType:  "group",
		MessageID: "om_1",
		RawText:   "/help",
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected single fallback help message, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Text, "@") {
		t.Fatalf("expected mention reminder, got %q", sender.messages[0].Text)
	}
}

func TestHandleIncomingMessage_ProjectCommandBindsTopic(t *testing.T) {
	topicStore := newFakeTopicStore()
	sender := &fakeMessageSender{returnThread: "omt_new"}
	mcp := &fakeMCPGateway{callText: "完成\n{\"threadId\":\"codex_t1\"}"}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: topicStore,
		Config: CommandServiceConfig{
			BotOpenID: "ou_bot",
			Heartbeat: time.Hour,
			ProjectAliasCWD: map[string]string{
				"tidb": "/work/tidb",
			},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /tidb 修复 flaky 测试",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	if len(mcp.callHistory) != 1 {
		t.Fatalf("expected one mcp tool call, got %d", len(mcp.callHistory))
	}
	if mcp.callHistory[0].tool != "codex" {
		t.Fatalf("expected codex tool call, got %q", mcp.callHistory[0].tool)
	}
	if gotCWD := mcp.callHistory[0].args["cwd"]; gotCWD != "/work/tidb" {
		t.Fatalf("unexpected cwd arg: %#v", gotCWD)
	}
	if gotPrompt := mcp.callHistory[0].args["prompt"]; gotPrompt == nil || !strings.Contains(gotPrompt.(string), "flaky") {
		t.Fatalf("unexpected prompt arg: %#v", gotPrompt)
	}
	if gotModel := mcp.callHistory[0].args["model"]; gotModel != "gpt-5.3-codex" {
		t.Fatalf("unexpected model arg: %#v", gotModel)
	}
	if gotSandbox := mcp.callHistory[0].args["sandbox"]; gotSandbox != "danger-full-access" {
		t.Fatalf("unexpected sandbox arg: %#v", gotSandbox)
	}
	if gotApproval := mcp.callHistory[0].args["approval-policy"]; gotApproval != "never" {
		t.Fatalf("unexpected approval-policy arg: %#v", gotApproval)
	}
	rawConfig, ok := mcp.callHistory[0].args["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config map in codex start args, got %#v", mcp.callHistory[0].args["config"])
	}
	if gotEffort := rawConfig["model_reasoning_effort"]; gotEffort != "xhigh" {
		t.Fatalf("unexpected reasoning effort in config: %#v", gotEffort)
	}

	binding, ok := topicStore.Get("oc_chat", "omt_new")
	if !ok {
		t.Fatal("expected topic binding to be persisted")
	}
	if binding.ProjectAlias != "tidb" || binding.CodexThreadID != "codex_t1" {
		t.Fatalf("unexpected binding: %+v", binding)
	}

	if len(sender.messages) < 1 {
		t.Fatalf("expected final response message, got %d", len(sender.messages))
	}
	if len(sender.reactions) != 1 {
		t.Fatalf("expected one processing reaction, got %d", len(sender.reactions))
	}
	if sender.reactions[0].EmojiType != "OnIt" {
		t.Fatalf("expected OnIt reaction, got %q", sender.reactions[0].EmojiType)
	}
}

func TestHandleIncomingMessage_TopicFollowupUsesBoundThread(t *testing.T) {
	topicStore := newFakeTopicStore()
	if err := topicStore.Upsert(TopicBinding{
		ChatID:         "oc_chat",
		FeishuThreadID: "omt_thread",
		ProjectAlias:   "tidb",
		CodexThreadID:  "codex_abc",
	}); err != nil {
		t.Fatalf("seed topic store: %v", err)
	}

	mcp := &fakeMCPGateway{callText: "followup ok"}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: topicStore,
		Config: CommandServiceConfig{
			BotOpenID:       "ou_bot",
			Heartbeat:       time.Hour,
			ProjectAliasCWD: map[string]string{"tidb": "/work/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:    "oc_chat",
		ThreadID:  "omt_thread",
		ChatType:  "group",
		MessageID: "om_2",
		RawText:   "继续刚才的话题",
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(mcp.callHistory) != 1 {
		t.Fatalf("expected one mcp call, got %d", len(mcp.callHistory))
	}
	if mcp.callHistory[0].tool != "codex-reply" {
		t.Fatalf("expected codex-reply tool, got %q", mcp.callHistory[0].tool)
	}
	if gotThreadID := mcp.callHistory[0].args["threadId"]; gotThreadID != "codex_abc" {
		t.Fatalf("unexpected codex thread id arg: %#v", gotThreadID)
	}
}

func TestHandleIncomingMessage_MCPCallInvalidJSON(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /mcp call echo {bad}",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(sender.messages) == 0 {
		t.Fatal("expected error message")
	}
	if !strings.Contains(sender.messages[len(sender.messages)-1].Text, "JSON") {
		t.Fatalf("expected json error message, got %q", sender.messages[len(sender.messages)-1].Text)
	}
	if !strings.Contains(sender.messages[len(sender.messages)-1].Text, "诊断ID") {
		t.Fatalf("expected diagnostic id in json error message, got %q", sender.messages[len(sender.messages)-1].Text)
	}
}

func TestHandleIncomingMessage_MCPCallToolErrorIsHandledWithoutPropagation(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP: &fakeMCPGateway{
			callErr: errors.New(`tool "codex" returned error: Failed to parse configuration for Codex tool: missing field "prompt"`),
		},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"x"}`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected one failure message, got %d", len(sender.messages))
	}
	if len(sender.reactions) != 1 {
		t.Fatalf("expected one processing reaction, got %d", len(sender.reactions))
	}
	if sender.reactions[0].EmojiType != "OnIt" {
		t.Fatalf("expected OnIt reaction, got %q", sender.reactions[0].EmojiType)
	}
	if !strings.Contains(sender.messages[0].Text, "调用工具失败") {
		t.Fatalf("expected tool failure message, got %q", sender.messages[0].Text)
	}
	if !strings.Contains(sender.messages[0].Text, "诊断ID") {
		t.Fatalf("expected diagnostic id in failure message, got %q", sender.messages[0].Text)
	}
}

func TestHandleIncomingMessage_MCPCallCodexRequiresPrompt(t *testing.T) {
	mcp := &fakeMCPGateway{}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {}`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected one validation message, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Text, "prompt") {
		t.Fatalf("expected prompt validation in response, got %q", sender.messages[0].Text)
	}
	if mcp.calledWith.tool != "" {
		t.Fatalf("expected no tool call for invalid codex args, got %q", mcp.calledWith.tool)
	}
}

func TestHandleIncomingMessage_MCPCallCodexExplainsNewThreadBehavior(t *testing.T) {
	mcp := &fakeMCPGateway{
		callText: "ok\n{\"threadId\":\"codex_t1\"}",
	}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"pwd"}`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected one response message, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Text, "/mcp call codex") {
		t.Fatalf("expected response to explain new-thread behavior, got %q", sender.messages[0].Text)
	}
	if !strings.Contains(sender.messages[0].Text, "每次都会新建") {
		t.Fatalf("expected response to mention new thread behavior, got %q", sender.messages[0].Text)
	}
}

func TestHandleIncomingMessage_MCPCallCodexDoesNotReuseTopicThread(t *testing.T) {
	topicStore := newFakeTopicStore()
	if err := topicStore.Upsert(TopicBinding{
		ChatID:         "oc_chat",
		FeishuThreadID: "omt_topic",
		ProjectAlias:   "tidb",
		CodexThreadID:  "codex_existing",
	}); err != nil {
		t.Fatalf("seed topic store: %v", err)
	}

	mcp := &fakeMCPGateway{
		callText: "ok\n{\n  \"threadId\": \"codex_next\"\n}",
	}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: topicStore,
		Config: CommandServiceConfig{
			BotOpenID:       "ou_bot",
			Heartbeat:       time.Hour,
			ProjectAliasCWD: map[string]string{"tidb": "/work/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"pwd"}`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if _, ok := mcp.calledWith.args["threadId"]; ok {
		t.Fatalf("expected no injected codex thread id, got %#v", mcp.calledWith.args["threadId"])
	}
	binding, ok := topicStore.Get("oc_chat", "omt_topic")
	if !ok {
		t.Fatal("expected topic binding to remain")
	}
	if binding.CodexThreadID != "codex_next" {
		t.Fatalf("expected updated codex thread id, got %q", binding.CodexThreadID)
	}
}

func TestHandleIncomingMessage_MCPCallCodexStripsManualThreadID(t *testing.T) {
	mcp := &fakeMCPGateway{
		callText: "ok\n{\"threadId\":\"codex_new\"}",
	}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"pwd","threadId":"codex_old"}`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if _, ok := mcp.calledWith.args["threadId"]; ok {
		t.Fatalf("expected threadId to be removed from codex start args, got %#v", mcp.calledWith.args["threadId"])
	}
}

func TestHandleIncomingMessage_CodexSlashResetsTopicThreadForFollowups(t *testing.T) {
	topicStore := newFakeTopicStore()
	mcp := &fakeMCPGateway{
		callTexts: []string{
			"first\n{\"threadId\":\"codex_t1\"}",
			"follow1",
			"second\n{\"threadId\":\"codex_t2\"}",
			"follow2",
		},
	}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: topicStore,
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	firstSlash := IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"first"}`,
		MentionedIDs: []string{"ou_bot"},
	}
	if err := svc.HandleIncomingMessage(context.Background(), firstSlash); err != nil {
		t.Fatalf("first slash command error: %v", err)
	}
	if err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:    "oc_chat",
		ThreadID:  "omt_topic",
		ChatType:  "group",
		MessageID: "om_2",
		RawText:   "follow a",
	}); err != nil {
		t.Fatalf("first followup error: %v", err)
	}
	secondSlash := IncomingMessage{
		ChatID:       "oc_chat",
		ThreadID:     "omt_topic",
		ChatType:     "group",
		MessageID:    "om_3",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"prompt":"second"}`,
		MentionedIDs: []string{"ou_bot"},
	}
	if err := svc.HandleIncomingMessage(context.Background(), secondSlash); err != nil {
		t.Fatalf("second slash command error: %v", err)
	}
	if err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:    "oc_chat",
		ThreadID:  "omt_topic",
		ChatType:  "group",
		MessageID: "om_4",
		RawText:   "follow b",
	}); err != nil {
		t.Fatalf("second followup error: %v", err)
	}

	if len(mcp.callHistory) != 4 {
		t.Fatalf("expected four mcp calls, got %d", len(mcp.callHistory))
	}
	if mcp.callHistory[0].tool != "codex" {
		t.Fatalf("first call should be codex, got %q", mcp.callHistory[0].tool)
	}
	if mcp.callHistory[1].tool != "codex-reply" {
		t.Fatalf("second call should be codex-reply, got %q", mcp.callHistory[1].tool)
	}
	if got := mcp.callHistory[1].args["threadId"]; got != "codex_t1" {
		t.Fatalf("first followup should use codex_t1, got %#v", got)
	}
	if mcp.callHistory[2].tool != "codex" {
		t.Fatalf("third call should be codex, got %q", mcp.callHistory[2].tool)
	}
	if _, ok := mcp.callHistory[2].args["threadId"]; ok {
		t.Fatalf("second slash call should not inject threadId, got %#v", mcp.callHistory[2].args["threadId"])
	}
	if mcp.callHistory[3].tool != "codex-reply" {
		t.Fatalf("fourth call should be codex-reply, got %q", mcp.callHistory[3].tool)
	}
	if got := mcp.callHistory[3].args["threadId"]; got != "codex_t2" {
		t.Fatalf("second followup should use codex_t2, got %#v", got)
	}
}

func TestHandleIncomingMessage_HeartbeatWhileProcessing(t *testing.T) {
	sender := &fakeMessageSender{}
	mcp := &fakeMCPGateway{callText: "ok\n{\"threadId\":\"codex_t\"}", callDelay: 80 * time.Millisecond}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        mcp,
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config: CommandServiceConfig{
			BotOpenID:       "ou_bot",
			Heartbeat:       20 * time.Millisecond,
			ProjectAliasCWD: map[string]string{"tidb": "/work/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /tidb 长任务",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	if len(sender.reactions) != 1 {
		t.Fatalf("expected one processing reaction, got %d", len(sender.reactions))
	}
	if sender.reactions[0].EmojiType != "OnIt" {
		t.Fatalf("expected OnIt reaction, got %q", sender.reactions[0].EmojiType)
	}
	if len(sender.messages) < 2 {
		t.Fatalf("expected heartbeat + final, got %d", len(sender.messages))
	}
	foundHeartbeat := false
	for _, msg := range sender.messages {
		if strings.Contains(msg.Text, "处理中") {
			foundHeartbeat = true
			if !strings.Contains(msg.Text, "已运行") {
				t.Fatalf("expected heartbeat to include elapsed duration, got %q", msg.Text)
			}
			break
		}
	}
	if !foundHeartbeat {
		t.Fatalf("expected heartbeat message, got %+v", sender.messages)
	}
}

func TestHandleIncomingMessage_DependencyErrorsReplyToUserWithoutPropagation(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP: &fakeMCPGateway{
			callErr: errors.New("boom"),
		},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config: CommandServiceConfig{
			BotOpenID:       "ou_bot",
			Heartbeat:       time.Hour,
			ProjectAliasCWD: map[string]string{"tidb": "/work/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /tidb 测试失败",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one failure message, got %d", len(sender.messages))
	}
	if len(sender.reactions) != 1 {
		t.Fatalf("expected one processing reaction, got %d", len(sender.reactions))
	}
	if sender.reactions[0].EmojiType != "OnIt" {
		t.Fatalf("expected OnIt reaction, got %q", sender.reactions[0].EmojiType)
	}
	if !strings.Contains(sender.messages[0].Text, "执行失败") {
		t.Fatalf("expected execution failure message, got %q", sender.messages[0].Text)
	}
	if !strings.Contains(sender.messages[0].Text, "诊断ID") {
		t.Fatalf("expected diagnostic id in execution failure message, got %q", sender.messages[0].Text)
	}
}

func TestHandleIncomingMessage_UsesConfiguredStartReaction(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{callText: "ok\n{\"threadId\":\"codex_t\"}"},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config: CommandServiceConfig{
			BotOpenID:               "ou_bot",
			Heartbeat:               time.Hour,
			StartProcessingReaction: "Typing",
			ProjectAliasCWD:         map[string]string{"tidb": "/work/tidb"},
		},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      "<at user_id=\"ou_bot\"></at> /tidb 长任务",
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}
	if len(sender.reactions) != 1 {
		t.Fatalf("expected one processing reaction, got %d", len(sender.reactions))
	}
	if sender.reactions[0].EmojiType != "Typing" {
		t.Fatalf("expected Typing reaction, got %q", sender.reactions[0].EmojiType)
	}
}
