package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type createMessageAPI interface {
	Create(ctx context.Context, req *larkim.CreateMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageResp, error)
}

type TextSender struct {
	api createMessageAPI
}

func NewTextSender(api createMessageAPI) *TextSender {
	return &TextSender{api: api}
}

func (s *TextSender) SendText(ctx context.Context, chatID, text string) error {
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("chat id is required")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("text is required")
	}

	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("marshal message content: %w", err)
	}

	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType("text").
		Content(string(content)).
		Build()

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(body).
		Build()
	req.Body = body

	resp, err := s.api.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create message request: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("create message response is nil")
	}
	if !resp.Success() {
		return fmt.Errorf("create message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}
