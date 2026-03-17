package service

import "testing"

func TestProcessMessage_EchoMode(t *testing.T) {
	processor := Processor{EchoMode: true, DefaultReply: "fallback"}
	got := processor.ProcessMessage("hello")
	if got != "hello" {
		t.Fatalf("expected echo text, got %q", got)
	}
}

func TestProcessMessage_DefaultReplyWhenEchoDisabled(t *testing.T) {
	processor := Processor{EchoMode: false, DefaultReply: "fallback"}
	got := processor.ProcessMessage("hello")
	if got != "fallback" {
		t.Fatalf("expected default reply, got %q", got)
	}
}

func TestProcessMessage_EmptyTextFallsBackToDefault(t *testing.T) {
	processor := Processor{EchoMode: true, DefaultReply: "fallback"}
	got := processor.ProcessMessage("   ")
	if got != "fallback" {
		t.Fatalf("expected fallback for empty text, got %q", got)
	}
}
