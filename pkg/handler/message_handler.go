package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/D3Hunter/frieren-clone/pkg/service"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type MessageService interface {
	HandleIncomingMessage(ctx context.Context, msg service.IncomingMessage) error
}

type MessageHandler struct {
	service           MessageService
	ignoreBotMessages bool
}

func NewMessageHandler(messageService MessageService, ignoreBotMessages bool) *MessageHandler {
	return &MessageHandler{
		service:           messageService,
		ignoreBotMessages: ignoreBotMessages,
	}
}

func (h *MessageHandler) HandleEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	if strings.TrimSpace(stringValue(message.MessageType)) != "text" {
		return nil
	}

	if h.ignoreBotMessages && event.Event.Sender != nil {
		senderType := strings.TrimSpace(stringValue(event.Event.Sender.SenderType))
		if senderType != "" && senderType != "user" {
			return nil
		}
	}

	chatID := strings.TrimSpace(stringValue(message.ChatId))
	if chatID == "" {
		return fmt.Errorf("event missing chat id")
	}
	messageID := strings.TrimSpace(stringValue(message.MessageId))
	if messageID == "" {
		return fmt.Errorf("event missing message id")
	}

	text, err := parseTextContent(stringValue(message.Content))
	if err != nil {
		return fmt.Errorf("parse text content: %w", err)
	}

	if err := h.service.HandleIncomingMessage(ctx, service.IncomingMessage{
		ChatID:       chatID,
		MessageID:    messageID,
		ThreadID:     strings.TrimSpace(stringValue(message.ThreadId)),
		ChatType:     strings.TrimSpace(stringValue(message.ChatType)),
		RawText:      text,
		MentionedIDs: extractMentionedOpenIDs(message.Mentions),
	}); err != nil {
		return fmt.Errorf("handle incoming message: %w", err)
	}
	return nil
}

func extractMentionedOpenIDs(mentions []*larkim.MentionEvent) []string {
	ids := make([]string, 0, len(mentions))
	for _, mention := range mentions {
		if mention == nil || mention.Id == nil || mention.Id.OpenId == nil {
			continue
		}
		id := strings.TrimSpace(*mention.Id.OpenId)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func parseTextContent(raw string) (string, error) {
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", err
	}
	return content.Text, nil
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
