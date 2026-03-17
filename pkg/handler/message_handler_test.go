package handler

import (
	"context"
	"errors"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeProcessor struct {
	lastText string
	out      string
}

func (f *fakeProcessor) ProcessMessage(text string) string {
	f.lastText = text
	if f.out != "" {
		return f.out
	}
	return text
}

type fakeSender struct {
	chatID string
	text   string
	err    error
	calls  int
}

func (f *fakeSender) SendText(ctx context.Context, chatID, text string) error {
	f.calls++
	f.chatID = chatID
	f.text = text
	return f.err
}

func TestHandleEvent_IgnoresNonTextMessages(t *testing.T) {
	processor := &fakeProcessor{}
	sender := &fakeSender{}
	h := NewMessageHandler(processor, sender, true)

	err := h.HandleEvent(context.Background(), newEvent("image", `{}`, "oc_x", "user"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected sender not called, got %d", sender.calls)
	}
}

func TestHandleEvent_IgnoresBotMessagesWhenConfigured(t *testing.T) {
	processor := &fakeProcessor{}
	sender := &fakeSender{}
	h := NewMessageHandler(processor, sender, true)

	err := h.HandleEvent(context.Background(), newEvent("text", `{"text":"hello"}`, "oc_x", "app"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected sender not called, got %d", sender.calls)
	}
}

func TestHandleEvent_SendsReplyForTextMessages(t *testing.T) {
	processor := &fakeProcessor{out: "processed"}
	sender := &fakeSender{}
	h := NewMessageHandler(processor, sender, true)

	err := h.HandleEvent(context.Background(), newEvent("text", `{"text":"hello"}`, "oc_x", "user"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processor.lastText != "hello" {
		t.Fatalf("expected processor text hello, got %q", processor.lastText)
	}
	if sender.chatID != "oc_x" || sender.text != "processed" {
		t.Fatalf("unexpected send args: chat=%q text=%q", sender.chatID, sender.text)
	}
}

func TestHandleEvent_PropagatesSenderError(t *testing.T) {
	processor := &fakeProcessor{out: "processed"}
	sender := &fakeSender{err: errors.New("boom")}
	h := NewMessageHandler(processor, sender, true)

	err := h.HandleEvent(context.Background(), newEvent("text", `{"text":"hello"}`, "oc_x", "user"))
	if err == nil {
		t.Fatal("expected sender error")
	}
}

func newEvent(msgType, content, chatID, senderType string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageType: strPtr(msgType),
				Content:     strPtr(content),
				ChatId:      strPtr(chatID),
			},
			Sender: &larkim.EventSender{
				SenderType: strPtr(senderType),
			},
		},
	}
}

func strPtr(v string) *string {
	return &v
}
