package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolInfo struct {
	Name        string
	Description string
}

type Gateway struct {
	endpoint string
	timeout  time.Duration
}

func NewGateway(endpoint string, timeout time.Duration) *Gateway {
	return &Gateway{endpoint: strings.TrimSpace(endpoint), timeout: timeout}
}

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

func (g *Gateway) CallTool(ctx context.Context, tool string, args map[string]any) (string, error) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "", fmt.Errorf("tool is required")
	}
	if args == nil {
		args = map[string]any{}
	}

	var output string
	err := g.withSession(ctx, func(callCtx context.Context, session *sdk.ClientSession) error {
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
	if strings.TrimSpace(g.endpoint) == "" {
		return fmt.Errorf("mcp endpoint is required")
	}
	callCtx := ctx
	cancel := func() {}
	if g.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, g.timeout)
	}
	defer cancel()

	client := sdk.NewClient(&sdk.Implementation{Name: "frieren-clone", Version: "1.0.0"}, nil)
	session, err := client.Connect(callCtx, &sdk.StreamableClientTransport{Endpoint: g.endpoint}, nil)
	if err != nil {
		return fmt.Errorf("connect mcp endpoint %q: %w", g.endpoint, err)
	}
	defer session.Close()

	if err := fn(callCtx, session); err != nil {
		return err
	}
	return nil
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
		return "调用成功（无输出）"
	}
	return strings.Join(parts, "\n")
}
