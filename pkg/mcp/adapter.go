package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolInfo is a simplified MCP tool descriptor exposed to the service layer.
type ToolInfo struct {
	Name        string
	Description string
}

// Gateway wraps a reusable MCP client session for list/schema/call operations.
type Gateway struct {
	endpoint           string
	timeout            time.Duration
	mu                 sync.Mutex
	session            *sdk.ClientSession
	sessionLastUsedAt  time.Time
}

// NewGateway builds a Gateway for the given streamable MCP endpoint and call timeout.
func NewGateway(endpoint string, timeout time.Duration) *Gateway {
	return &Gateway{
		endpoint: strings.TrimSpace(endpoint),
		timeout:  timeout,
	}
}

// Close closes the active MCP client session, if any.
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closeSessionLocked()
}

// ListTools lists all MCP tools by paging through the server cursor until completion.
func (g *Gateway) ListTools(ctx context.Context) ([]ToolInfo, error) {
	tools, err := g.fetchTools(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ToolInfo, 0, len(tools))
	for _, tool := range tools {
		result = append(result, ToolInfo{Name: tool.Name, Description: tool.Description})
	}
	return result, nil
}

// GetToolSchema returns the formatted JSON input schema for a named MCP tool.
func (g *Gateway) GetToolSchema(ctx context.Context, tool string) (string, error) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "", fmt.Errorf("tool is required")
	}
	tools, err := g.fetchTools(ctx)
	if err != nil {
		return "", err
	}
	for _, item := range tools {
		if item.Name != tool {
			continue
		}
		formatted, err := json.MarshalIndent(item.InputSchema, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal schema for tool %q: %w", tool, err)
		}
		return string(formatted), nil
	}
	return "", fmt.Errorf("tool %q not found", tool)
}

// CallTool executes a named MCP tool with JSON arguments and renders textual output.
func (g *Gateway) CallTool(ctx context.Context, tool string, args map[string]any) (string, error) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "", fmt.Errorf("tool is required")
	}
	if args == nil {
		args = map[string]any{}
	}

	var output string
	err := g.withSessionWithTimeout(ctx, g.timeoutForTool(tool), func(callCtx context.Context, session *sdk.ClientSession) error {
		result, err := session.CallTool(callCtx, &sdk.CallToolParams{
			Name:      tool,
			Arguments: args,
		})
		if err != nil {
			return fmt.Errorf("call tool %q: %w", tool, err)
		}
		text := renderCallToolResult(result)
		if result.IsError {
			return fmt.Errorf("tool %q returned error: %s", tool, text)
		}
		output = text
		return nil
	})
	if err != nil {
		return "", err
	}
	return output, nil
}

func (g *Gateway) fetchTools(ctx context.Context) ([]*sdk.Tool, error) {
	var tools []*sdk.Tool
	err := g.withSession(ctx, func(callCtx context.Context, session *sdk.ClientSession) error {
		cursor := ""
		for {
			result, err := session.ListTools(callCtx, &sdk.ListToolsParams{Cursor: cursor})
			if err != nil {
				return fmt.Errorf("list tools: %w", err)
			}
			tools = append(tools, result.Tools...)
			if strings.TrimSpace(result.NextCursor) == "" {
				break
			}
			cursor = result.NextCursor
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tools, nil
}

func (g *Gateway) withSession(ctx context.Context, fn func(context.Context, *sdk.ClientSession) error) error {
	return g.withSessionWithTimeout(ctx, g.timeout, fn)
}

func (g *Gateway) withSessionWithTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context, *sdk.ClientSession) error) error {
	if strings.TrimSpace(g.endpoint) == "" {
		return fmt.Errorf("mcp endpoint is required")
	}
	callCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	g.mu.Lock()
	defer g.mu.Unlock()

	session, err := g.ensureSessionLocked(callCtx)
	if err != nil {
		return err
	}
	runWithSession := func(activeSession *sdk.ClientSession) error {
		g.sessionLastUsedAt = time.Now()
		if err := fn(callCtx, activeSession); err != nil {
			return err
		}
		g.sessionLastUsedAt = time.Now()
		return nil
	}
	if err := runWithSession(session); err != nil {
		if !isSessionConnectionClosedError(err) {
			return err
		}
		// When the underlying stream transport is gone (server restart/shutdown), the cached
		// SDK session can stay in a closing state and every later call will fail immediately.
		// Drop it and retry once with a freshly connected session.
		if closeErr := g.closeSessionLocked(); closeErr != nil {
			return fmt.Errorf("reset mcp session after connection closure: %w", closeErr)
		}
		reconnectedSession, reconnectErr := g.ensureSessionLocked(callCtx)
		if reconnectErr != nil {
			return reconnectErr
		}
		if retryErr := runWithSession(reconnectedSession); retryErr != nil {
			return retryErr
		}
	}
	return nil
}

func (g *Gateway) ensureSessionLocked(ctx context.Context) (*sdk.ClientSession, error) {
	if g.session != nil {
		return g.session, nil
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "frieren-clone", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: g.endpoint}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect mcp endpoint %q: %w", g.endpoint, err)
	}
	g.session = session
	g.sessionLastUsedAt = time.Now()
	return g.session, nil
}

func (g *Gateway) closeSessionLocked() error {
	if g.session == nil {
		return nil
	}
	err := g.session.Close()
	g.session = nil
	g.sessionLastUsedAt = time.Time{}
	return err
}

func (g *Gateway) timeoutForTool(tool string) time.Duration {
	if isCodexExecutionTool(tool) {
		return 0
	}
	return g.timeout
}

func isCodexExecutionTool(tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	return tool == "codex" || tool == "codex-reply"
}

func isSessionConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "client is closing") ||
		strings.Contains(lowered, "failed to reconnect") ||
		(strings.Contains(lowered, "connection closed") && strings.Contains(lowered, "tools/call"))
}

func renderCallToolResult(result *sdk.CallToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content)+1)
	for _, content := range result.Content {
		switch value := content.(type) {
		case *sdk.TextContent:
			text := strings.TrimSpace(value.Text)
			if text != "" {
				parts = append(parts, text)
			}
		default:
			encoded, err := json.MarshalIndent(content, "", "  ")
			if err == nil && len(encoded) > 0 {
				parts = append(parts, string(encoded))
			}
		}
	}
	if result.StructuredContent != nil {
		encoded, err := json.MarshalIndent(result.StructuredContent, "", "  ")
		if err == nil && len(encoded) > 0 {
			parts = append(parts, string(encoded))
		}
	}
	if len(parts) == 0 {
		return "Call succeeded (no output)."
	}
	return strings.Join(parts, "\n")
}
