package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/D3Hunter/frieren-clone/pkg/service"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeMessageService struct {
	last  service.IncomingMessage
	calls int
	err   error
}

func (f *fakeMessageService) HandleIncomingMessage(ctx context.Context, msg service.IncomingMessage) error {
	f.last = msg
	f.calls++
	return f.err
}

func TestHandleEvent_IgnoresNonTextMessages(t *testing.T) {
	svc := &fakeMessageService{}
	h := NewMessageHandler(svc, true)

	err := h.HandleEvent(context.Background(), newEvent("image", `{}`, "oc_x", "user", nil, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.last.ChatID != "" {
		t.Fatalf("service should not be called, got %+v", svc.last)
	}
}

func TestHandleEvent_IgnoresBotMessagesWhenConfigured(t *testing.T) {
	svc := &fakeMessageService{}
	h := NewMessageHandler(svc, true)

	err := h.HandleEvent(context.Background(), newEvent("text", `{"text":"hello"}`, "oc_x", "app", nil, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.last.ChatID != "" {
		t.Fatalf("service should not be called, got %+v", svc.last)
	}
}

func TestHandleEvent_ParsesMentionsAndThread(t *testing.T) {
	svc := &fakeMessageService{}
	h := NewMessageHandler(svc, true)

	mentions := []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: strPtr("ou_bot")}}}
	err := h.HandleEvent(context.Background(), newEvent("text", `{"text":"<at user_id=\"ou_bot\"></at> /help"}`, "oc_x", "user", mentions, "omt_topic"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.last.ChatID != "oc_x" {
		t.Fatalf("unexpected chat id: %q", svc.last.ChatID)
	}
	if svc.last.ThreadID != "omt_topic" {
		t.Fatalf("unexpected thread id: %q", svc.last.ThreadID)
	}
	if len(svc.last.MentionedIDs) != 1 || svc.last.MentionedIDs[0] != "ou_bot" {
		t.Fatalf("unexpected mentioned ids: %+v", svc.last.MentionedIDs)
	}
}

func TestHandleEvent_DeduplicatesSameMessageID(t *testing.T) {
	svc := &fakeMessageService{}
	h := NewMessageHandler(svc, true)

	event := newEvent("text", `{"text":"hello"}`, "oc_x", "user", nil, "")
	if err := h.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("first HandleEvent error: %v", err)
	}
	if err := h.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("second HandleEvent error: %v", err)
	}

	if svc.calls != 1 {
		t.Fatalf("expected one service call after duplicate events, got %d", svc.calls)
	}
}

func TestHandleEvent_PropagatesServiceError(t *testing.T) {
	svc := &fakeMessageService{err: errors.New("boom")}
	h := NewMessageHandler(svc, true)

	err := h.HandleEvent(context.Background(), newEvent("text", `{"text":"hello"}`, "oc_x", "user", nil, ""))
	if err == nil {
		t.Fatal("expected service error")
	}
}

func newEvent(msgType, content, chatID, senderType string, mentions []*larkim.MentionEvent, threadID string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_x"),
				MessageType: strPtr(msgType),
				Content:     strPtr(content),
				ChatId:      strPtr(chatID),
				ThreadId:    strPtr(threadID),
				Mentions:    mentions,
			},
			Sender: &larkim.EventSender{SenderType: strPtr(senderType)},
		},
	}
}

func strPtr(v string) *string {
	return &v
}
