package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/D3Hunter/frieren-clone/pkg/config"
	"github.com/D3Hunter/frieren-clone/pkg/mcp"
	"github.com/D3Hunter/frieren-clone/pkg/sender"
	"github.com/D3Hunter/frieren-clone/pkg/service"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"go.uber.org/zap"
)

const (
	simulationModeEnv    = "FRIEREN_SIMULATION_MODE"
	simulationRoundsEnv  = "FRIEREN_SIMULATION_ROUNDS"
	simulationRealMCPEnv = "FRIEREN_SIMULATION_REAL_MCP"

	defaultSimulationRounds = 1

	simulationPrompt = "/tidb give me a markdown example that you can output, include all support markdown elements in your output format, just to test. also give the level values for title. be longer than 2k"
)

var unsupportedFeishuHeadingPattern = regexp.MustCompile(`(?m)^\s*#{5,6}\s+\S+`)

func simulationModeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(simulationModeEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func simulationRounds() (int, error) {
	raw := strings.TrimSpace(os.Getenv(simulationRoundsEnv))
	if raw == "" {
		return defaultSimulationRounds, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", simulationRoundsEnv, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", simulationRoundsEnv)
	}
	return value, nil
}

func simulationUseRealMCP() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(simulationRealMCPEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func runSimulationMode(cfg config.Config, logger *zap.Logger) error {
	rounds, err := simulationRounds()
	if err != nil {
		return err
	}

	mcpGateway := service.MCPGateway(&simulationMCPGateway{logger: logger.Named("simulation.mcp")})
	if simulationUseRealMCP() {
		realGateway := mcp.NewGateway(cfg.MCP.Endpoint, time.Duration(cfg.MCP.TimeoutSec)*time.Second)
		mcpGateway = mcpGatewayAdapter{gateway: realGateway}
		logger.Info("simulation configured to use real mcp gateway", zap.String("endpoint", cfg.MCP.Endpoint))
	} else {
		logger.Info("simulation configured to use mocked mcp gateway")
	}

	messageAPI := &simulationMessageAPI{logger: logger.Named("simulation.sender")}
	reactionAPI := &simulationReactionAPI{logger: logger.Named("simulation.reaction")}
	textSender := sender.NewTextSender(messageAPI, reactionAPI)
	commandService := service.NewCommandService(service.CommandServiceDeps{
		MCP:    mcpGateway,
		Sender: messageSenderAdapter{sender: textSender},
		Logger: logger.Named("service"),
		Config: service.CommandServiceConfig{
			BotOpenID:               cfg.Commands.BotOpenID,
			Heartbeat:               time.Duration(cfg.Commands.HeartbeatSec) * time.Second,
			StartProcessingReaction: cfg.Commands.StartReaction,
			ProjectAliasCWD:         simulationProjectAliasMap(cfg.Projects),
		},
	})

	logger.Info("simulation mode started", zap.Int("rounds", rounds), zap.String("prompt", simulationPrompt))
	for round := 1; round <= rounds; round++ {
		if err := commandService.HandleIncomingMessage(context.Background(), service.IncomingMessage{
			ChatID:    "oc_simulated_chat",
			ChatType:  "p2p",
			MessageID: fmt.Sprintf("om_sim_%03d", round),
			RawText:   simulationPrompt,
		}); err != nil {
			return fmt.Errorf("simulation round %d failed: %w", round, err)
		}
		logger.Info("simulation round completed", zap.Int("round", round))
	}

	logger.Info(
		"simulation mode finished",
		zap.Int("rounds", rounds),
		zap.Int("reply_count", messageAPI.replyCount),
		zap.Int("interactive_reply_count", messageAPI.interactiveReplyCount),
		zap.Int("failure_count", messageAPI.failureCount),
		zap.Int("unrendered_count", messageAPI.unrenderedCount),
	)
	if messageAPI.failureCount > 0 {
		return fmt.Errorf("detected %d command failures in simulation replies", messageAPI.failureCount)
	}
	if messageAPI.unrenderedCount > 0 {
		return fmt.Errorf("detected %d unrendered markdown chunks in simulation replies", messageAPI.unrenderedCount)
	}
	return nil
}

func simulationProjectAliasMap(projects map[string]config.ProjectConfig) map[string]string {
	aliasCWD := make(map[string]string, len(projects))
	for alias, project := range projects {
		aliasCWD[strings.ToLower(strings.TrimSpace(alias))] = strings.TrimSpace(project.CWD)
	}
	return aliasCWD
}

type simulationMCPGateway struct {
	logger      *zap.Logger
	codexRuns   int
	markdownDoc string
}

func (m *simulationMCPGateway) ListTools(context.Context) ([]service.ToolInfo, error) {
	return []service.ToolInfo{
		{Name: "codex", Description: "start a codex run"},
		{Name: "codex-status", Description: "query codex context window"},
	}, nil
}

func (m *simulationMCPGateway) GetToolSchema(context.Context, string) (string, error) {
	return "{}", nil
}

func (m *simulationMCPGateway) CallTool(_ context.Context, tool string, args map[string]any) (string, error) {
	tool = strings.ToLower(strings.TrimSpace(tool))
	switch tool {
	case "codex":
		m.codexRuns++
		threadID := fmt.Sprintf("codex_sim_thread_%03d", m.codexRuns)
		if m.markdownDoc == "" {
			m.markdownDoc = buildSimulationMarkdownDocument()
		}
		parts := splitRunes(m.markdownDoc, 320)
		content := make([]map[string]any, 0, len(parts))
		for _, part := range parts {
			content = append(content, map[string]any{
				"type": "output_text",
				"text": part,
			})
		}
		payload := map[string]any{
			"response": map[string]any{
				"thread_id": threadID,
				"output": []map[string]any{
					{
						"type":    "message",
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal simulation codex payload: %w", err)
		}
		if m.logger != nil {
			m.logger.Info("simulation mcp codex call", zap.String("thread_id", threadID), zap.Int("content_parts", len(parts)))
		}
		return "simulated codex structured output\n" + string(encoded), nil
	case "codex-status":
		return `{"contextWindow":{"usedTokens":23000,"maxTokens":258000}}`, nil
	default:
		return "", fmt.Errorf("simulation mcp tool not supported: %s", tool)
	}
}

type simulationMessageAPI struct {
	logger                *zap.Logger
	replyCount            int
	interactiveReplyCount int
	failureCount          int
	unrenderedCount       int
}

func (m *simulationMessageAPI) Create(context.Context, *larkim.CreateMessageReq, ...larkcore.RequestOptionFunc) (*larkim.CreateMessageResp, error) {
	return &larkim.CreateMessageResp{
		CodeError: larkcore.CodeError{Code: 0},
		Data:      &larkim.CreateMessageRespData{ThreadId: strPtr("omt_simulation")},
	}, nil
}

func (m *simulationMessageAPI) Reply(_ context.Context, req *larkim.ReplyMessageReq, _ ...larkcore.RequestOptionFunc) (*larkim.ReplyMessageResp, error) {
	m.replyCount++
	if req == nil || req.Body == nil || req.Body.MsgType == nil {
		return nil, fmt.Errorf("simulation reply request missing message type")
	}

	msgType := strings.TrimSpace(*req.Body.MsgType)
	content := ""
	if req.Body.Content != nil {
		content = *req.Body.Content
	}

	switch msgType {
	case "interactive":
		m.interactiveReplyCount++
		markdown, err := extractInteractiveMarkdownContent(content)
		if err != nil {
			m.unrenderedCount++
			if m.logger != nil {
				m.logger.Error("failed to decode interactive markdown content", zap.Error(err))
			}
			break
		}
		if hasUnrenderedArtifacts(markdown) {
			m.unrenderedCount++
			if m.logger != nil {
				m.logger.Error("unrendered markdown artifact detected", zap.String("preview", previewRunes(markdown, 240)))
			}
		}
	default:
		if isSimulationFailureReply(content) {
			m.failureCount++
			if m.logger != nil {
				m.logger.Error("simulation command failure reply detected", zap.String("preview", previewRunes(content, 240)))
			}
		}
		if hasUnrenderedArtifacts(content) {
			m.unrenderedCount++
			if m.logger != nil {
				m.logger.Error("unrendered text artifact detected", zap.String("preview", previewRunes(content, 240)))
			}
		}
	}

	return &larkim.ReplyMessageResp{
		CodeError: larkcore.CodeError{Code: 0},
		Data:      &larkim.ReplyMessageRespData{ThreadId: strPtr("omt_simulation")},
	}, nil
}

type simulationReactionAPI struct {
	logger *zap.Logger
}

func (r *simulationReactionAPI) Create(_ context.Context, req *larkim.CreateMessageReactionReq, _ ...larkcore.RequestOptionFunc) (*larkim.CreateMessageReactionResp, error) {
	if r.logger != nil && req != nil && req.Body != nil && req.Body.ReactionType != nil && req.Body.ReactionType.EmojiType != nil {
		r.logger.Info("simulation reaction added", zap.String("emoji", strings.TrimSpace(*req.Body.ReactionType.EmojiType)))
	}
	return &larkim.CreateMessageReactionResp{CodeError: larkcore.CodeError{Code: 0}}, nil
}

func extractInteractiveMarkdownContent(content string) (string, error) {
	var payload struct {
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return "", fmt.Errorf("decode interactive payload: %w", err)
	}
	for _, element := range payload.Body.Elements {
		if strings.TrimSpace(element.Tag) == "markdown" {
			return element.Content, nil
		}
	}
	return "", fmt.Errorf("interactive payload does not contain markdown element")
}

func hasUnrenderedArtifacts(text string) bool {
	for _, marker := range []string{
		`"messages": [`,
		`"response": {`,
		`"role": "assistant"`,
		`"type": "output_text"`,
		`"thread_id":`,
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if hasUnsupportedHeadingOutsideFencedCode(text) {
		return true
	}
	return hasUnbalancedFencedCodeDelimiters(text)
}

func hasUnsupportedHeadingOutsideFencedCode(text string) bool {
	inFence := false
	var fenceChar rune
	fenceLen := 0

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if inFence {
			if isFenceCloser(trimmed, fenceChar, fenceLen) {
				inFence = false
				fenceChar = 0
				fenceLen = 0
			}
			continue
		}
		if markerChar, markerLen, ok := parseFenceMarker(trimmed); ok {
			inFence = true
			fenceChar = markerChar
			fenceLen = markerLen
			continue
		}
		// Feishu markdown cards have unreliable rendering for h5/h6; treat them as compatibility artifacts.
		if unsupportedFeishuHeadingPattern.MatchString(line) {
			return true
		}
	}
	return false
}

func hasUnbalancedFencedCodeDelimiters(text string) bool {
	inFence := false
	var fenceChar rune
	fenceLen := 0

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if inFence {
			if isFenceCloser(trimmed, fenceChar, fenceLen) {
				inFence = false
				fenceChar = 0
				fenceLen = 0
			}
			continue
		}
		if markerChar, markerLen, ok := parseFenceMarker(trimmed); ok {
			inFence = true
			fenceChar = markerChar
			fenceLen = markerLen
		}
	}
	return inFence
}

func parseFenceMarker(line string) (rune, int, bool) {
	if len(line) < 3 {
		return 0, 0, false
	}
	first := rune(line[0])
	if first != '`' && first != '~' {
		return 0, 0, false
	}

	count := 0
	for _, r := range line {
		if r != first {
			break
		}
		count++
	}
	if count < 3 {
		return 0, 0, false
	}
	return first, count, true
}

func isFenceCloser(line string, fenceChar rune, fenceLen int) bool {
	if fenceChar == 0 || fenceLen < 3 {
		return false
	}
	if line == "" {
		return false
	}
	count := 0
	for _, r := range line {
		if r != fenceChar {
			break
		}
		count++
	}
	if count < fenceLen {
		return false
	}
	return strings.TrimSpace(line[count:]) == ""
}

func isSimulationFailureReply(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return strings.Contains(text, "Execution failed:") && strings.Contains(text, "Diagnostic ID:")
}

func previewRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "..."
}

func splitRunes(text string, maxRunes int) []string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}
	runes := []rune(text)
	chunks := make([]string, 0, len(runes)/maxRunes+1)
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			chunks = append(chunks, string(runes))
			break
		}
		chunks = append(chunks, string(runes[:maxRunes]))
		runes = runes[maxRunes:]
	}
	return chunks
}

func buildSimulationMarkdownDocument() string {
	longParagraph := strings.Repeat(
		"Markdown rendering should preserve structure, wrapping, links, and code formatting across long outputs while keeping readability in both desktop and mobile clients. ",
		26,
	)

	sections := []string{
		"# Markdown Full-Surface Test Document",
		"This response is intentionally long (over 2,000 characters) and covers core markdown elements in one output.",
		"## Title Levels",
		"| Level Value | Syntax | HTML Tag |\n| --- | --- | --- |\n| 1 | `# H1` | `<h1>` |\n| 2 | `## H2` | `<h2>` |\n| 3 | `### H3` | `<h3>` |\n| 4 | `#### H4` | `<h4>` |\n| 5 | `##### H5` | `<h5>` |\n| 6 | `###### H6` | `<h6>` |",
		"## Headings Showcase\n# H1\n## H2\n### H3\n#### H4\n##### H5\n###### H6",
		"## Text Styles\nNormal text, **bold**, *italic*, ***bold italic***, ~~strikethrough~~, and `inline code`.",
		"## Blockquote\n> Quoted line one.\n> Quoted line two with **emphasis** and `code`.",
		"## Lists\n- unordered A\n- unordered B\n\n1. ordered A\n2. ordered B\n\n- [x] task done\n- [ ] task pending",
		"## Links and Images\n[PingCAP](https://www.pingcap.com)\n\n![Markdown image](https://picsum.photos/640/120)",
		"## Code Block\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello markdown\")\n}\n```",
		"## Table\n| Name | Value |\n| --- | --- |\n| alpha | 1 |\n| beta | 2 |",
		"## Horizontal Rule\n---",
		"## Long Paragraph\n" + longParagraph,
	}
	return strings.Join(sections, "\n\n")
}

func strPtr(value string) *string {
	return &value
}
