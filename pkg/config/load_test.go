package config

import (
	"strings"
	"testing"
)

func TestLoadFromBytes_RequiresAppCredentials(t *testing.T) {
	_, err := LoadFromBytes([]byte("[app]\nid=''\nsecret=''\n"))
	if err == nil {
		t.Fatal("expected error for missing app credentials")
	}

	if !strings.Contains(err.Error(), "app.id") {
		t.Fatalf("expected app.id in error, got %v", err)
	}
}

func TestLoadFromBytes_AppliesDefaults(t *testing.T) {
	cfg, err := LoadFromBytes([]byte("[app]\nid='cli_x'\nsecret='s_x'\n"))
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}

	if cfg.Message.DefaultReply == "" {
		t.Fatal("expected default reply to be set")
	}
	if !cfg.Message.EchoMode {
		t.Fatal("expected echo mode to default true")
	}
	if !cfg.Message.IgnoreBotMessages {
		t.Fatal("expected ignore_bot_messages to default true")
	}
	if cfg.LongConn.LogLevel != "info" {
		t.Fatalf("expected long_conn.log_level to default info, got %q", cfg.LongConn.LogLevel)
	}
}
