package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const defaultMaxChunkRunes = 1800

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

	chunks := splitChunks(text, s.maxChunkRunes)
	if len(chunks) > 1 {
		for i, chunk := range chunks {
			chunks[i] = fmt.Sprintf("[%d/%d] %s", i+1, len(chunks), chunk)
		}
	}

	lastThreadID := strings.TrimSpace(req.ThreadID)
	for _, chunk := range chunks {
		msgType := selectMsgType(chunk)
		content, err := buildContent(msgType, chunk)
		if err != nil {
			return SendReceipt{}, err
		}

		threadID, err := s.sendOne(ctx, chatID, strings.TrimSpace(req.ReplyToMessageID), msgType, content)
		if err != nil {
			return SendReceipt{}, err
		}
		if strings.TrimSpace(threadID) != "" {
			lastThreadID = strings.TrimSpace(threadID)
		}
	}

	return SendReceipt{ThreadID: lastThreadID}, nil
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

func selectMsgType(text string) string {
	_ = text
	return "text"
}

func buildContent(msgType, text string) (string, error) {
	_ = msgType
	encoded, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", fmt.Errorf("marshal text content: %w", err)
	}
	return string(encoded), nil
}

func splitChunks(input string, maxRunes int) []string {
	if maxRunes <= 0 || utf8.RuneCountInString(input) <= maxRunes {
		return []string{input}
	}
	chunks := []string{}
	runes := []rune(input)
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, strings.TrimSpace(string(runes[start:end])))
	}
	return chunks
}
