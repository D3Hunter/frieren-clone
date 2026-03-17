package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMCPGateway struct {
	tools      []ToolInfo
	schemaText string
	callText   string
	callErr    error
	calledWith struct {
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
	f.calledWith.tool = tool
	f.calledWith.args = args
	if f.callErr != nil {
		return "", f.callErr
	}
	if f.callText == "" {
		return "ok", nil
	}
	return f.callText, nil
}

type fakeCodexGateway struct {
	startThreadID string
	startOutput   string
	replyOutput   string
	err           error
	delay         time.Duration

	startCalls []struct {
		cwd    string
		prompt string
	}
	replyCalls []struct {
		cwd      string
		threadID string
		prompt   string
	}
}

func (f *fakeCodexGateway) Start(ctx context.Context, cwd, prompt string) (string, string, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.startCalls = append(f.startCalls, struct {
		cwd    string
		prompt string
	}{cwd: cwd, prompt: prompt})
	if f.err != nil {
		return "", "", f.err
	}
	if f.startThreadID == "" {
		f.startThreadID = "codex_thread_new"
	}
	if f.startOutput == "" {
		f.startOutput = "done"
	}
	return f.startThreadID, f.startOutput, nil
}

func (f *fakeCodexGateway) Reply(ctx context.Context, cwd, threadID, prompt string) (string, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.replyCalls = append(f.replyCalls, struct {
		cwd      string
		threadID string
		prompt   string
	}{cwd: cwd, threadID: threadID, prompt: prompt})
	if f.err != nil {
		return "", f.err
	}
	if f.replyOutput == "" {
		f.replyOutput = "reply done"
	}
	return f.replyOutput, nil
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

func TestHandleIncomingMessage_HelpCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Codex:      &fakeCodexGateway{},
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

func TestHandleIncomingMessage_GroupCommandRequiresMention(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Codex:      &fakeCodexGateway{},
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
	codex := &fakeCodexGateway{startThreadID: "codex_t1", startOutput: "完成"}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Codex:      codex,
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

	if len(codex.startCalls) != 1 {
		t.Fatalf("expected one codex start call, got %d", len(codex.startCalls))
	}
	if codex.startCalls[0].cwd != "/work/tidb" {
		t.Fatalf("unexpected cwd: %q", codex.startCalls[0].cwd)
	}
	if !strings.Contains(codex.startCalls[0].prompt, "flaky") {
		t.Fatalf("unexpected prompt: %q", codex.startCalls[0].prompt)
	}

	binding, ok := topicStore.Get("oc_chat", "omt_new")
	if !ok {
		t.Fatal("expected topic binding to be persisted")
	}
	if binding.ProjectAlias != "tidb" || binding.CodexThreadID != "codex_t1" {
		t.Fatalf("unexpected binding: %+v", binding)
	}

	if len(sender.messages) < 2 {
		t.Fatalf("expected progress + final messages, got %d", len(sender.messages))
	}
	if sender.messages[0].Text != "⏳" {
		t.Fatalf("expected first message to be hourglass, got %q", sender.messages[0].Text)
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

	codex := &fakeCodexGateway{replyOutput: "followup ok"}
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Codex:      codex,
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
	if len(codex.replyCalls) != 1 {
		t.Fatalf("expected one codex reply call, got %d", len(codex.replyCalls))
	}
	if codex.replyCalls[0].threadID != "codex_abc" {
		t.Fatalf("unexpected codex thread id: %q", codex.replyCalls[0].threadID)
	}
}

func TestHandleIncomingMessage_MCPCallInvalidJSON(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Codex:      &fakeCodexGateway{},
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
}

func TestHandleIncomingMessage_MCPCallToolErrorIsHandledWithoutPropagation(t *testing.T) {
	sender := &fakeMessageSender{}
	svc := NewCommandService(CommandServiceDeps{
		MCP: &fakeMCPGateway{
			callErr: errors.New(`tool "codex" returned error: Failed to parse configuration for Codex tool: missing field "prompt"`),
		},
		Codex:      &fakeCodexGateway{},
		Sender:     sender,
		TopicStore: newFakeTopicStore(),
		Config:     CommandServiceConfig{BotOpenID: "ou_bot", Heartbeat: time.Hour},
	})

	err := svc.HandleIncomingMessage(context.Background(), IncomingMessage{
		ChatID:       "oc_chat",
		ChatType:     "group",
		MessageID:    "om_1",
		RawText:      `<at user_id="ou_bot"></at> /mcp call codex {"topic":"x"}`,
		MentionedIDs: []string{"ou_bot"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingMessage error: %v", err)
	}

	if len(sender.messages) != 2 {
		t.Fatalf("expected hourglass and one failure message, got %d", len(sender.messages))
	}
	if sender.messages[0].Text != "⏳" {
		t.Fatalf("expected first message hourglass, got %q", sender.messages[0].Text)
	}
	if !strings.Contains(sender.messages[1].Text, "调用工具失败") {
		t.Fatalf("expected tool failure message, got %q", sender.messages[1].Text)
	}
}

func TestHandleIncomingMessage_HeartbeatWhileProcessing(t *testing.T) {
	sender := &fakeMessageSender{}
	codex := &fakeCodexGateway{startThreadID: "codex_t", startOutput: "ok", delay: 80 * time.Millisecond}
	svc := NewCommandService(CommandServiceDeps{
		MCP:        &fakeMCPGateway{},
		Codex:      codex,
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

	if len(sender.messages) < 3 {
		t.Fatalf("expected hourglass + heartbeat + final, got %d", len(sender.messages))
	}
	if sender.messages[0].Text != "⏳" {
		t.Fatalf("expected first message hourglass, got %q", sender.messages[0].Text)
	}
	foundHeartbeat := false
	for _, msg := range sender.messages[1:] {
		if strings.Contains(msg.Text, "处理中") {
			foundHeartbeat = true
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
		MCP:        &fakeMCPGateway{},
		Codex:      &fakeCodexGateway{err: errors.New("boom")},
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
	if len(sender.messages) != 2 {
		t.Fatalf("expected hourglass and one failure message, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[1].Text, "执行失败") {
		t.Fatalf("expected execution failure message, got %q", sender.messages[1].Text)
	}
}
