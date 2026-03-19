package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const defaultMaxChunkRunes = 1800
const defaultMaxMarkdownChunkRunes = 1380

const (
	renderModePlainText     = "plain_text"
	renderModeCodexMarkdown = "codex_markdown"
)

var markdownListItemPattern = regexp.MustCompile(`^\s{0,3}(?:[-*+]|\d+[.)])\s+`)
var markdownTableSeparatorPattern = regexp.MustCompile(`^\s*\|?(?:\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$`)

type messageAPI interface {
	// Create sends a new chat message using Feishu's create-message API.
	Create(ctx context.Context, req *larkim.CreateMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageResp, error)
	// Reply sends a reply message attached to an existing Feishu message thread.
	Reply(ctx context.Context, req *larkim.ReplyMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.ReplyMessageResp, error)
}

type messageReactionAPI interface {
	// Create creates one emoji reaction on a target Feishu message.
	Create(ctx context.Context, req *larkim.CreateMessageReactionReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageReactionResp, error)
}

// SendRequest describes one outgoing reply workflow request.
type SendRequest struct {
	ChatID           string
	ReplyToMessageID string
	ThreadID         string
	Text             string
	RenderMode       string
}

// AddReactionRequest describes one emoji reaction request on an existing message.
type AddReactionRequest struct {
	MessageID string
	EmojiType string
}

// SendReceipt reports response metadata from Send.
type SendReceipt struct {
	ThreadID string
}

// TextSender sends Feishu text replies and reactions with chunking and thread support.
type TextSender struct {
	api           messageAPI
	reactionAPI   messageReactionAPI
	maxChunkRunes int
}

// NewTextSender builds a TextSender with default chunk size and API dependencies.
func NewTextSender(api messageAPI, reactionAPI messageReactionAPI) *TextSender {
	return &TextSender{api: api, reactionAPI: reactionAPI, maxChunkRunes: defaultMaxChunkRunes}
}

// SetMaxChunkRunesForTest overrides chunk size in tests to force multi-chunk behavior.
func (s *TextSender) SetMaxChunkRunesForTest(max int) {
	if max > 0 {
		s.maxChunkRunes = max
	}
}

// Send validates input, splits long text, and sends each chunk as a Feishu reply in order.
func (s *TextSender) Send(ctx context.Context, req SendRequest) (SendReceipt, error) {
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		return SendReceipt{}, fmt.Errorf("chat id is required")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return SendReceipt{}, fmt.Errorf("text is required")
	}

	renderMode := normalizeRenderMode(req.RenderMode)
	if renderMode == renderModeCodexMarkdown {
		translated, err := translateCodexMarkdownToFeishu(text)
		if err != nil {
			return SendReceipt{}, fmt.Errorf("translate markdown for feishu: %w", err)
		}
		text = translated
	}

	chunks := splitChunks(text, s.maxChunkRunes)
	if renderMode == renderModeCodexMarkdown {
		markdownChunkRunes := s.maxChunkRunes
		// Feishu interactive markdown can fail on larger chunk payloads even when plain text would pass.
		// Use a safer markdown-specific cap to avoid fallback-to-plain and preserve rendering consistency.
		if markdownChunkRunes > defaultMaxMarkdownChunkRunes {
			markdownChunkRunes = defaultMaxMarkdownChunkRunes
		}
		chunks = splitMarkdownChunks(text, markdownChunkRunes)
	}
	if len(chunks) > 1 {
		for i, chunk := range chunks {
			chunks[i] = withChunkPrefix(chunk, i, len(chunks), renderMode)
		}
	}

	lastThreadID := strings.TrimSpace(req.ThreadID)
	for _, chunk := range chunks {
		threadID, err := s.sendChunk(ctx, chatID, strings.TrimSpace(req.ReplyToMessageID), chunk, renderMode)
		if err != nil {
			return SendReceipt{}, err
		}
		if strings.TrimSpace(threadID) != "" {
			lastThreadID = strings.TrimSpace(threadID)
		}
	}

	return SendReceipt{ThreadID: lastThreadID}, nil
}

func (s *TextSender) sendChunk(ctx context.Context, chatID, replyToMessageID, text, renderMode string) (string, error) {
	if renderMode != renderModeCodexMarkdown {
		content, err := buildContent("text", text)
		if err != nil {
			return "", err
		}
		return s.sendOne(ctx, chatID, replyToMessageID, "text", content)
	}

	interactiveContent, err := buildContent("interactive", text)
	if err != nil {
		return "", err
	}
	threadID, sendErr := s.sendOne(ctx, chatID, replyToMessageID, "interactive", interactiveContent)
	if sendErr == nil {
		return threadID, nil
	}

	plainContent, plainErr := buildContent("text", text)
	if plainErr != nil {
		return "", fmt.Errorf("send markdown card failed: %w (plain-text fallback encode failed: %v)", sendErr, plainErr)
	}
	threadID, fallbackErr := s.sendOne(ctx, chatID, replyToMessageID, "text", plainContent)
	if fallbackErr != nil {
		return "", fmt.Errorf("send markdown card failed: %w (plain-text fallback failed: %v)", sendErr, fallbackErr)
	}
	return threadID, nil
}

// AddReaction adds an emoji reaction to an existing Feishu message.
func (s *TextSender) AddReaction(ctx context.Context, req AddReactionRequest) error {
	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		return fmt.Errorf("message id is required")
	}
	emojiType := strings.TrimSpace(req.EmojiType)
	if emojiType == "" {
		return fmt.Errorf("emoji type is required")
	}
	if s.reactionAPI == nil {
		return fmt.Errorf("message reaction api is required")
	}

	body := larkim.NewCreateMessageReactionReqBodyBuilder().
		ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
		Build()
	createReq := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(body).
		Build()
	createReq.Body = body
	resp, err := s.reactionAPI.Create(ctx, createReq)
	if err != nil {
		return fmt.Errorf("create message reaction request: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("create message reaction response is nil")
	}
	if !resp.Success() {
		return fmt.Errorf("create message reaction failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (s *TextSender) sendOne(ctx context.Context, chatID, replyToMessageID, msgType, content string) (string, error) {
	if replyToMessageID != "" {
		replyInThread := true
		body := larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			ReplyInThread(replyInThread).
			Build()
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(replyToMessageID).
			Body(body).
			Build()
		req.Body = body
		resp, err := s.api.Reply(ctx, req)
		if err != nil {
			return "", fmt.Errorf("reply message request: %w", err)
		}
		if resp == nil {
			return "", fmt.Errorf("reply message response is nil")
		}
		if !resp.Success() {
			return "", fmt.Errorf("reply message failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		if resp.Data != nil && resp.Data.ThreadId != nil {
			return *resp.Data.ThreadId, nil
		}
		return "", nil
	}

	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(msgType).
		Content(content).
		Build()
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(body).
		Build()
	req.Body = body
	resp, err := s.api.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create message request: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("create message response is nil")
	}
	if !resp.Success() {
		return "", fmt.Errorf("create message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data != nil && resp.Data.ThreadId != nil {
		return *resp.Data.ThreadId, nil
	}
	return "", nil
}

func buildContent(msgType, text string) (string, error) {
	if msgType == "interactive" {
		text = normalizeMarkdown(text)
		encoded, err := json.Marshal(map[string]any{
			"schema": "2.0",
			"config": map[string]any{
				"wide_screen_mode": true,
			},
			"body": map[string]any{
				"elements": []map[string]any{
					{
						"tag":     "markdown",
						"content": text,
					},
				},
			},
		})
		if err != nil {
			return "", fmt.Errorf("marshal interactive content: %w", err)
		}
		return string(encoded), nil
	}

	encoded, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", fmt.Errorf("marshal text content: %w", err)
	}
	return string(encoded), nil
}

func normalizeMarkdown(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func normalizeRenderMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == renderModeCodexMarkdown {
		return renderModeCodexMarkdown
	}
	return renderModePlainText
}

func withChunkPrefix(chunk string, index, total int, renderMode string) string {
	if normalizeRenderMode(renderMode) == renderModeCodexMarkdown {
		// Feishu markdown headings can be degraded when a plain-text ordering label precedes the block.
		// Keep markdown syntax at the top of each chunk and append the ordering marker as a suffix.
		return fmt.Sprintf("%s\n\n[%d/%d]", strings.TrimRight(chunk, "\n"), index+1, total)
	}
	return fmt.Sprintf("[%d/%d] %s", index+1, total, chunk)
}

func splitChunks(input string, maxRunes int) []string {
	if maxRunes <= 0 || utf8.RuneCountInString(input) <= maxRunes {
		return []string{input}
	}

	remaining := []rune(input)
	chunks := make([]string, 0, len(remaining)/maxRunes+1)
	for len(remaining) > 0 {
		if len(remaining) <= maxRunes {
			chunks = append(chunks, string(remaining))
			break
		}

		cut := chooseChunkCut(remaining, maxRunes)
		chunks = append(chunks, string(remaining[:cut]))
		remaining = remaining[cut:]
	}
	return chunks
}

func chooseChunkCut(runes []rune, maxRunes int) int {
	if len(runes) <= maxRunes {
		return len(runes)
	}

	// Prefer keeping whole lines to preserve list and paragraph readability.
	for i := maxRunes - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}

	// If a single line is too long, keep whole words when possible.
	for i := maxRunes - 1; i >= 0; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1
		}
	}

	// Last resort for overlong single tokens (for example very long URLs).
	return maxRunes
}

func splitMarkdownChunks(input string, maxRunes int) []string {
	input = normalizeMarkdown(input)
	if maxRunes <= 0 || utf8.RuneCountInString(input) <= maxRunes {
		return []string{input}
	}

	blocks := splitMarkdownBlocks(input)
	if len(blocks) == 0 {
		return splitChunks(input, maxRunes)
	}

	chunks := make([]string, 0, len(blocks))
	currentBlocks := make([]string, 0, 8)
	currentRunes := 0
	flushCurrent := func() {
		if len(currentBlocks) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(currentBlocks, ""))
		currentBlocks = currentBlocks[:0]
		currentRunes = 0
	}
	appendBlock := func(block string) {
		currentBlocks = append(currentBlocks, block)
		currentRunes += utf8.RuneCountInString(block)
	}

	for _, block := range blocks {
		if block == "" {
			continue
		}
		blockRunes := utf8.RuneCountInString(block)

		if len(currentBlocks) == 0 {
			if blockRunes <= maxRunes {
				appendBlock(block)
				continue
			}
			forced := splitChunks(block, maxRunes)
			chunks = append(chunks, forced...)
			continue
		}

		if currentRunes+blockRunes <= maxRunes {
			appendBlock(block)
			continue
		}

		if headingTail, ok := headingCarryTailBlocks(currentBlocks, block); ok {
			// Keep heading lines attached to their following block. Without this carry-over, a table/list/code
			// block can start a new chunk with no section title, and some Feishu markdown cards render poorly.
			tailRunes := runeCountOfBlocks(headingTail)
			currentBlocks = currentBlocks[:len(currentBlocks)-len(headingTail)]
			currentRunes -= tailRunes
			flushCurrent()

			currentBlocks = append(currentBlocks, headingTail...)
			currentRunes = tailRunes
			if currentRunes+blockRunes <= maxRunes {
				appendBlock(block)
				continue
			}

			flushCurrent()
			if blockRunes <= maxRunes {
				appendBlock(block)
				continue
			}
			forced := splitChunks(block, maxRunes)
			chunks = append(chunks, forced...)
			continue
		}

		flushCurrent()
		if blockRunes <= maxRunes {
			appendBlock(block)
			continue
		}
		forced := splitChunks(block, maxRunes)
		chunks = append(chunks, forced...)
	}

	flushCurrent()
	if len(chunks) == 0 {
		return []string{input}
	}
	return chunks
}

func headingCarryTailBlocks(currentBlocks []string, nextBlock string) ([]string, bool) {
	if len(currentBlocks) == 0 {
		return nil, false
	}
	if strings.TrimSpace(nextBlock) == "" {
		return nil, false
	}
	lastContent := len(currentBlocks) - 1
	for lastContent >= 0 && strings.TrimSpace(currentBlocks[lastContent]) == "" {
		lastContent--
	}
	if lastContent < 0 || !isHeadingBlock(currentBlocks[lastContent]) {
		return nil, false
	}
	return append([]string(nil), currentBlocks[lastContent:]...), true
}

func isHeadingBlock(block string) bool {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return false
	}

	hashCount := 0
	for hashCount < len(trimmed) && trimmed[hashCount] == '#' {
		hashCount++
	}
	if hashCount == 0 || hashCount > 6 {
		return false
	}
	if len(trimmed) == hashCount {
		return false
	}
	return trimmed[hashCount] == ' '
}

func runeCountOfBlocks(blocks []string) int {
	total := 0
	for _, block := range blocks {
		total += utf8.RuneCountInString(block)
	}
	return total
}

func splitMarkdownBlocks(input string) []string {
	lines := strings.SplitAfter(input, "\n")
	if len(lines) == 0 {
		return []string{input}
	}
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	blocks := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		line := trimLineEnding(lines[i])
		trimmed := strings.TrimSpace(line)
		start := i

		if trimmed == "" {
			for i < len(lines) && strings.TrimSpace(trimLineEnding(lines[i])) == "" {
				i++
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		if fenceChar, fenceLen, ok := parseFenceMarker(trimmed); ok {
			i++
			for i < len(lines) {
				next := strings.TrimSpace(trimLineEnding(lines[i]))
				i++
				if isFenceCloser(next, fenceChar, fenceLen) {
					break
				}
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		if i+1 < len(lines) && isTableHeaderLine(trimLineEnding(lines[i])) && isTableSeparatorLine(trimLineEnding(lines[i+1])) {
			i += 2
			for i < len(lines) && isTableRowLine(trimLineEnding(lines[i])) {
				i++
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		if isListItemLine(line) {
			i++
			for i < len(lines) {
				current := trimLineEnding(lines[i])
				trimmedCurrent := strings.TrimSpace(current)
				if trimmedCurrent == "" {
					break
				}
				if fenceChar, fenceLen, ok := parseFenceMarker(strings.TrimSpace(current)); ok {
					i++
					for i < len(lines) {
						next := strings.TrimSpace(trimLineEnding(lines[i]))
						i++
						if isFenceCloser(next, fenceChar, fenceLen) {
							break
						}
					}
					continue
				}
				if isListItemLine(current) || isListContinuationLine(current) {
					i++
					continue
				}
				break
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		i++
		for i < len(lines) {
			current := trimLineEnding(lines[i])
			if strings.TrimSpace(current) == "" {
				break
			}
			if _, _, ok := parseFenceMarker(strings.TrimSpace(current)); ok {
				break
			}
			if i+1 < len(lines) && isTableHeaderLine(current) && isTableSeparatorLine(trimLineEnding(lines[i+1])) {
				break
			}
			if isListItemLine(current) {
				break
			}
			i++
		}
		blocks = append(blocks, strings.Join(lines[start:i], ""))
	}
	return blocks
}

func trimLineEnding(line string) string {
	return strings.TrimRight(line, "\n")
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
		if r == first {
			count++
			continue
		}
		break
	}
	if count < 3 {
		return 0, 0, false
	}
	return first, count, true
}

func isFenceCloser(line string, fenceChar rune, fenceLen int) bool {
	if len(line) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	count := 0
	for _, r := range trimmed {
		if r == fenceChar {
			count++
			continue
		}
		break
	}
	return count >= fenceLen
}

func isListItemLine(line string) bool {
	return markdownListItemPattern.MatchString(line)
}

func isListContinuationLine(line string) bool {
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

func isTableHeaderLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if !strings.Contains(trimmed, "|") {
		return false
	}
	return !markdownTableSeparatorPattern.MatchString(trimmed)
}

func isTableSeparatorLine(line string) bool {
	return markdownTableSeparatorPattern.MatchString(strings.TrimSpace(line))
}

func isTableRowLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if markdownTableSeparatorPattern.MatchString(trimmed) {
		return false
	}
	return strings.Contains(trimmed, "|")
}
