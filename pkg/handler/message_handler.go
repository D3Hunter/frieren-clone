package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type Processor interface {
	ProcessMessage(text string) string
}

type Sender interface {
	SendText(ctx context.Context, chatID, text string) error
}

type MessageHandler struct {
	processor         Processor
	sender            Sender
	ignoreBotMessages bool
}

func NewMessageHandler(processor Processor, sender Sender, ignoreBotMessages bool) *MessageHandler {
	return &MessageHandler{
		processor:         processor,
		sender:            sender,
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

	text, err := parseTextContent(stringValue(message.Content))
	if err != nil {
		return fmt.Errorf("parse text content: %w", err)
	}

	reply := strings.TrimSpace(h.processor.ProcessMessage(text))
	if reply == "" {
		return nil
	}

	if err := h.sender.SendText(ctx, chatID, reply); err != nil {
		return fmt.Errorf("send text: %w", err)
	}
	return nil
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
