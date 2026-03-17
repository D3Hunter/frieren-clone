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
	Create(ctx context.Context, req *larkim.CreateMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageResp, error)
	Reply(ctx context.Context, req *larkim.ReplyMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.ReplyMessageResp, error)
}

type SendRequest struct {
	ChatID           string
	ReplyToMessageID string
	ThreadID         string
	Text             string
}

type SendReceipt struct {
	ThreadID string
}

type TextSender struct {
	api           messageAPI
	maxChunkRunes int
}

func NewTextSender(api messageAPI) *TextSender {
	return &TextSender{api: api, maxChunkRunes: defaultMaxChunkRunes}
}

func (s *TextSender) SetMaxChunkRunesForTest(max int) {
	if max > 0 {
		s.maxChunkRunes = max
	}
}

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
	trimmed := strings.TrimSpace(text)
	if looksLikeMarkdown(trimmed) {
		return "text"
	}
	if utf8.RuneCountInString(trimmed) <= 120 {
		return "post"
	}
	return "text"
}

func looksLikeMarkdown(text string) bool {
	markers := []string{"```", "`", "#", "*", "|", "- ", "1.", "[", "]("}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func buildContent(msgType, text string) (string, error) {
	if msgType == "post" {
		payload := map[string]any{
			"zh_cn": map[string]any{
				"title": "Frieren",
				"content": [][]map[string]string{{
					{"tag": "text", "text": text},
				}},
			},
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("marshal post content: %w", err)
		}
		return string(encoded), nil
	}
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
