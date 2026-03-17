package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/D3Hunter/frieren-clone/pkg/service"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"go.uber.org/zap"
)

type MessageService interface {
	HandleIncomingMessage(ctx context.Context, msg service.IncomingMessage) error
}

type MessageHandler struct {
	service           MessageService
	ignoreBotMessages bool
	logger            *zap.Logger

	dedupeMu      sync.Mutex
	seenMessageAt map[string]time.Time
	dedupeWindow  time.Duration
}

const defaultMessageDedupeWindow = 10 * time.Minute

func NewMessageHandler(messageService MessageService, ignoreBotMessages bool, loggers ...*zap.Logger) *MessageHandler {
	logger := zap.NewNop()
	for _, item := range loggers {
		if item != nil {
			logger = item
			break
		}
	}
	return &MessageHandler{
		service:           messageService,
		ignoreBotMessages: ignoreBotMessages,
		logger:            logger,
		seenMessageAt:     map[string]time.Time{},
		dedupeWindow:      defaultMessageDedupeWindow,
	}
}

func (h *MessageHandler) HandleEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		h.logger.Info("received empty message event")
		return nil
	}

	message := event.Event.Message
	messageType := strings.TrimSpace(stringValue(message.MessageType))
	chatID := strings.TrimSpace(stringValue(message.ChatId))
	messageID := strings.TrimSpace(stringValue(message.MessageId))
	threadID := strings.TrimSpace(stringValue(message.ThreadId))
	chatType := strings.TrimSpace(stringValue(message.ChatType))
	senderType := ""
	if event.Event.Sender != nil {
		senderType = strings.TrimSpace(stringValue(event.Event.Sender.SenderType))
	}
	mentionedIDs := extractMentionedOpenIDs(message.Mentions)
	trace := service.EnsureTraceIDs(service.IncomingMessage{
		ChatID:    chatID,
		MessageID: messageID,
		ThreadID:  threadID,
	})
	requestID := strings.TrimSpace(trace.RequestID)
	correlationID := strings.TrimSpace(trace.CorrelationID)

	h.logger.Info(
		"received feishu event",
		zap.String("chat_id", chatID),
		zap.String("chat_type", chatType),
		zap.String("message_id", messageID),
		zap.String("thread_id", threadID),
		zap.String("topic_id", threadID),
		zap.String("message_type", messageType),
		zap.String("sender_type", senderType),
		zap.String("request_id", requestID),
		zap.String("correlation_id", correlationID),
		zap.Strings("mentioned_ids", mentionedIDs),
	)

	if messageType != "text" {
		h.logger.Info(
			"ignored non-text message event",
			zap.String("chat_id", chatID),
			zap.String("message_id", messageID),
			zap.String("message_type", messageType),
			zap.String("request_id", requestID),
			zap.String("correlation_id", correlationID),
		)
		return nil
	}

	if h.ignoreBotMessages && event.Event.Sender != nil {
		if senderType != "" && senderType != "user" {
			h.logger.Info(
				"ignored bot message event",
				zap.String("chat_id", chatID),
				zap.String("message_id", messageID),
				zap.String("sender_type", senderType),
				zap.String("request_id", requestID),
				zap.String("correlation_id", correlationID),
			)
			return nil
		}
	}

	if chatID == "" {
		return fmt.Errorf("event missing chat id")
	}
	if messageID == "" {
		return fmt.Errorf("event missing message id")
	}
	if h.isDuplicateEvent(chatID, messageID) {
		h.logger.Info(
			"ignored duplicated message event",
			zap.String("chat_id", chatID),
			zap.String("message_id", messageID),
			zap.String("thread_id", threadID),
			zap.String("topic_id", threadID),
			zap.String("request_id", requestID),
			zap.String("correlation_id", correlationID),
		)
		return nil
	}

	text, err := parseTextContent(stringValue(message.Content))
	if err != nil {
		h.logger.Error(
			"parse text message content failed",
			zap.String("chat_id", chatID),
			zap.String("message_id", messageID),
			zap.String("thread_id", threadID),
			zap.String("topic_id", threadID),
			zap.String("request_id", requestID),
			zap.String("correlation_id", correlationID),
			zap.Error(err),
		)
		return fmt.Errorf("parse text content: %w", err)
	}
	text = strings.TrimSpace(text)

	h.logger.Info(
		"dispatching text message to command service",
		zap.String("chat_id", chatID),
		zap.String("chat_type", chatType),
		zap.String("message_id", messageID),
		zap.String("thread_id", threadID),
		zap.String("topic_id", threadID),
		zap.String("request_id", requestID),
		zap.String("correlation_id", correlationID),
		zap.String("text", text),
		zap.Strings("mentioned_ids", mentionedIDs),
	)

	if err := h.service.HandleIncomingMessage(ctx, service.IncomingMessage{
		ChatID:        chatID,
		MessageID:     messageID,
		ThreadID:      threadID,
		ChatType:      chatType,
		RawText:       text,
		MentionedIDs:  mentionedIDs,
		RequestID:     requestID,
		CorrelationID: correlationID,
	}); err != nil {
		h.logger.Error(
			"command service returned error",
			zap.String("chat_id", chatID),
			zap.String("message_id", messageID),
			zap.String("thread_id", threadID),
			zap.String("topic_id", threadID),
			zap.String("request_id", requestID),
			zap.String("correlation_id", correlationID),
			zap.Error(err),
		)
		return fmt.Errorf("handle incoming message: %w", err)
	}
	h.logger.Info(
		"command service handled message",
		zap.String("chat_id", chatID),
		zap.String("message_id", messageID),
		zap.String("thread_id", threadID),
		zap.String("topic_id", threadID),
		zap.String("request_id", requestID),
		zap.String("correlation_id", correlationID),
	)
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

func (h *MessageHandler) isDuplicateEvent(chatID, messageID string) bool {
	chatID = strings.TrimSpace(chatID)
	messageID = strings.TrimSpace(messageID)
	if chatID == "" || messageID == "" {
		return false
	}
	now := time.Now()
	cutoff := now.Add(-h.dedupeWindow)
	key := chatID + "::" + messageID

	h.dedupeMu.Lock()
	defer h.dedupeMu.Unlock()

	for seenKey, seenAt := range h.seenMessageAt {
		if seenAt.Before(cutoff) {
			delete(h.seenMessageAt, seenKey)
		}
	}
	if _, exists := h.seenMessageAt[key]; exists {
		return true
	}
	h.seenMessageAt[key] = now
	return false
}
