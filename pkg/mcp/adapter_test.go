package mcp

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
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
	_, err := gateway.CallTool(context.Background(), "echo", map[string]any{"text": " "})
	if err == nil {
		t.Fatal("expected tool error")
	}
	if !strings.Contains(err.Error(), "text required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
