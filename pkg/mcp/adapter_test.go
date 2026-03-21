package mcp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type closeRecorderEventStore struct {
	inner  *sdk.MemoryEventStore
	closed atomic.Int32
}

func newCloseRecorderEventStore() *closeRecorderEventStore {
	return &closeRecorderEventStore{inner: sdk.NewMemoryEventStore(nil)}
}

func (s *closeRecorderEventStore) Open(ctx context.Context, sessionID, streamID string) error {
	return s.inner.Open(ctx, sessionID, streamID)
}

func (s *closeRecorderEventStore) Append(ctx context.Context, sessionID, streamID string, data []byte) error {
	return s.inner.Append(ctx, sessionID, streamID, data)
}

func (s *closeRecorderEventStore) After(ctx context.Context, sessionID, streamID string, index int) iter.Seq2[[]byte, error] {
	return s.inner.After(ctx, sessionID, streamID, index)
}

func (s *closeRecorderEventStore) SessionClosed(ctx context.Context, sessionID string) error {
	s.closed.Add(1)
	return s.inner.SessionClosed(ctx, sessionID)
}

func TestGateway_ListToolsAndSchemaAndCall(t *testing.T) {
	eventStore := newCloseRecorderEventStore()
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo text"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		Text string `json:"text"`
	}) (*sdk.CallToolResult, map[string]string, error) {
		if strings.TrimSpace(in.Text) == "" {
			return nil, nil, errors.New("text required")
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "echo: " + in.Text}}}, map[string]string{"echo": in.Text}, nil
	})

	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, &sdk.StreamableHTTPOptions{EventStore: eventStore})
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	gateway := NewGateway(httpServer.URL, 3*time.Second)
	ctx := context.Background()

	tools, err := gateway.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	schema, err := gateway.GetToolSchema(ctx, "echo")
	if err != nil {
		t.Fatalf("GetToolSchema error: %v", err)
	}
	schemaText := schema
	if !strings.Contains(schemaText, "text") {
		t.Fatalf("schema missing property text: %s", schemaText)
	}

	result, err := gateway.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !strings.Contains(result, "echo: hello") {
		t.Fatalf("unexpected call result: %q", result)
	}
	if err := gateway.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if eventStore.closed.Load() == 0 {
		t.Fatal("expected session close to be called")
	}
}

func TestGateway_CallTool_PropagatesToolErrors(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo text"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		Text string `json:"text"`
	}) (*sdk.CallToolResult, map[string]string, error) {
		if strings.TrimSpace(in.Text) == "" {
			return nil, nil, errors.New("text required")
		}
		return nil, map[string]string{"echo": in.Text}, nil
	})

	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, nil))
	defer httpServer.Close()

	gateway := NewGateway(httpServer.URL, 3*time.Second)
	defer func() {
		if err := gateway.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()
	_, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": " "})
	if err == nil {
		t.Fatal("expected tool error")
	}
	if !strings.Contains(err.Error(), "text required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_CallTool_ReusesSessionForSessionScopedFollowupTools(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	var (
		mu            sync.Mutex
		sessionThread = map[string]string{}
		startThreadID string
	)
	sdk.AddTool(server, &sdk.Tool{Name: "codex", Description: "start codex session"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		Prompt string `json:"prompt"`
	}) (*sdk.CallToolResult, map[string]any, error) {
		threadID := "thread-" + req.Session.ID()
		mu.Lock()
		sessionThread[req.Session.ID()] = threadID
		startThreadID = threadID
		mu.Unlock()
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, map[string]any{"threadId": threadID}, nil
	})
	sdk.AddTool(server, &sdk.Tool{Name: "codex-reply", Description: "continue codex session"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		ThreadID string `json:"threadId"`
		Prompt   string `json:"prompt"`
	}) (*sdk.CallToolResult, map[string]any, error) {
		mu.Lock()
		expectedThreadID, ok := sessionThread[req.Session.ID()]
		mu.Unlock()
		if !ok || expectedThreadID != in.ThreadID {
			return nil, nil, fmt.Errorf("Session not found for thread_id: %s", in.ThreadID)
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "reply ok"}}}, map[string]any{"threadId": in.ThreadID}, nil
	})

	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, nil))
	defer httpServer.Close()

	gateway := NewGateway(httpServer.URL, 3*time.Second)
	defer func() {
		if err := gateway.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()
	ctx := context.Background()

	if _, err := gateway.CallTool(ctx, "codex", map[string]any{"prompt": "start"}); err != nil {
		t.Fatalf("CallTool codex error: %v", err)
	}

	mu.Lock()
	threadID := startThreadID
	mu.Unlock()
	if strings.TrimSpace(threadID) == "" {
		t.Fatal("expected non-empty thread id from codex start call")
	}

	if _, err := gateway.CallTool(ctx, "codex-reply", map[string]any{
		"threadId": threadID,
		"prompt":   "follow up",
	}); err != nil {
		t.Fatalf("CallTool codex-reply error: %v", err)
	}
}

func TestGateway_CallTool_RecreatesSessionAfterIdleTimeout(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	var (
		mu         sync.Mutex
		sessionIDs []string
	)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo text"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		Text string `json:"text"`
	}) (*sdk.CallToolResult, map[string]any, error) {
		mu.Lock()
		sessionIDs = append(sessionIDs, req.Session.ID())
		mu.Unlock()
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: in.Text}}}, nil, nil
	})

	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, nil))
	defer httpServer.Close()

	gateway := NewGateway(httpServer.URL, 3*time.Second)
	gateway.sessionIdleTimeout = 50 * time.Millisecond
	defer func() {
		if err := gateway.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()

	if _, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": "first"}); err != nil {
		t.Fatalf("first CallTool error: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": "second"}); err != nil {
		t.Fatalf("second CallTool error: %v", err)
	}
	time.Sleep(70 * time.Millisecond)
	if _, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": "third"}); err != nil {
		t.Fatalf("third CallTool error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sessionIDs) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(sessionIDs))
	}
	if sessionIDs[0] != sessionIDs[1] {
		t.Fatalf("expected first two calls to share session, got %q and %q", sessionIDs[0], sessionIDs[1])
	}
	if sessionIDs[1] == sessionIDs[2] {
		t.Fatalf("expected third call to use a new session after timeout, got %q", sessionIDs[2])
	}
}

func TestGateway_CallTool_CodexReplyIgnoresGatewayTimeout(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "codex-reply", Description: "continue codex session"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		ThreadID string `json:"threadId"`
		Prompt   string `json:"prompt"`
	}) (*sdk.CallToolResult, map[string]any, error) {
		time.Sleep(80 * time.Millisecond)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "reply ok"}}}, map[string]any{"threadId": in.ThreadID}, nil
	})

	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, nil))
	defer httpServer.Close()

	gateway := NewGateway(httpServer.URL, 30*time.Millisecond)
	defer func() {
		if err := gateway.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()

	if _, err := gateway.CallTool(context.Background(), "codex-reply", map[string]any{
		"threadId": "thread-1",
		"prompt":   "continue",
	}); err != nil {
		t.Fatalf("CallTool codex-reply should not be bounded by gateway timeout, got error: %v", err)
	}
}

func TestGateway_CallTool_ReconnectsWhenCachedSessionIsClosed(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echo text"}, func(ctx context.Context, req *sdk.CallToolRequest, in struct {
		Text string `json:"text"`
	}) (*sdk.CallToolResult, map[string]any, error) {
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: "echo: " + strings.TrimSpace(in.Text)}},
		}, nil, nil
	})

	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, nil))
	defer httpServer.Close()

	gateway := NewGateway(httpServer.URL, 3*time.Second)
	defer func() {
		if err := gateway.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()

	if _, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": "first"}); err != nil {
		t.Fatalf("first CallTool error: %v", err)
	}
	if gateway.session == nil {
		t.Fatal("expected cached session after first call")
	}
	if err := gateway.session.Close(); err != nil {
		t.Fatalf("close cached session: %v", err)
	}

	result, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": "second"})
	if err != nil {
		t.Fatalf("second CallTool should reconnect with a fresh session, got error: %v", err)
	}
	if !strings.Contains(result, "echo: second") {
		t.Fatalf("unexpected second CallTool result: %q", result)
	}
}
