package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/D3Hunter/frieren-clone/pkg/feishumarkdown"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const defaultMaxChunkRunes = 1800
const defaultMaxMarkdownChunkRunes = 1380

const (
	renderModePlainText     = "plain_text"
	renderModeCodexMarkdown = "codex_markdown"
)

var prepareCodexMarkdown = feishumarkdown.PrepareCodexMarkdown

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
	chunks := splitChunks(text, s.maxChunkRunes)
	if renderMode == renderModeCodexMarkdown {
		preparedChunks, err := s.prepareCodexChunks(text)
		if err != nil {
			return SendReceipt{}, err
		}
		chunks = preparedChunks
	}
	if renderMode != renderModeCodexMarkdown && len(chunks) > 1 {
		for i, chunk := range chunks {
			chunks[i] = withPlainTextChunkPrefix(chunk, i, len(chunks))
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

func (s *TextSender) prepareCodexChunks(text string) ([]string, error) {
	markdownChunkRunes := s.maxChunkRunes
	// Feishu interactive markdown can fail on larger chunk payloads even when plain text would pass.
	// Keep the sender-side cap so codex markdown still matches the pre-library delivery budget.
	if markdownChunkRunes > defaultMaxMarkdownChunkRunes {
		markdownChunkRunes = defaultMaxMarkdownChunkRunes
	}

	prepared, err := prepareCodexMarkdown(text, feishumarkdown.PrepareOptions{MaxChunkRunes: markdownChunkRunes})
	if err != nil {
		return nil, fmt.Errorf("prepare markdown for feishu: %w", err)
	}

	chunks := make([]string, 0, len(prepared.Chunks))
	for _, chunk := range prepared.Chunks {
		chunks = append(chunks, chunk.Content)
	}
	return chunks, nil
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

func withPlainTextChunkPrefix(chunk string, index, total int) string {
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
