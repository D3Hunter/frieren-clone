package config

import (
	"strings"
	"testing"
)

func TestLoadFromBytes_ParsesProjectsAndCommandConfig(t *testing.T) {
	raw := []byte(`
[app]
id='cli_x'
secret='sec'

[commands]
bot_open_id='ou_bot'
heartbeat_sec=120
start_reaction='Typing'

[runtime]
topic_state_file='./tmp/topic-state.json'

[logging]
format='json'

[projects.tidb]
cwd='/Users/me/tidb'

[projects.play]
cwd='/Users/me/play'
`)

	cfg, err := LoadFromBytes(raw)
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}

	if cfg.Commands.BotOpenID != "ou_bot" {
		t.Fatalf("unexpected bot open id: %q", cfg.Commands.BotOpenID)
	}
	if cfg.Commands.HeartbeatSec != 120 {
		t.Fatalf("unexpected heartbeat: %d", cfg.Commands.HeartbeatSec)
	}
	if cfg.Commands.StartReaction != "Typing" {
		t.Fatalf("unexpected start reaction: %q", cfg.Commands.StartReaction)
	}
	if cfg.Runtime.TopicStateFile != "./tmp/topic-state.json" {
		t.Fatalf("unexpected topic state file: %q", cfg.Runtime.TopicStateFile)
	}
	if cfg.Logging.Format != "json" {
		t.Fatalf("unexpected logging format: %q", cfg.Logging.Format)
	}
	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cfg.Projects))
	}
	if cfg.Projects["tidb"].CWD != "/Users/me/tidb" {
		t.Fatalf("unexpected tidb cwd: %q", cfg.Projects["tidb"].CWD)
	}
}

func TestLoadFromBytes_AppliesMCPAndCommandDefaults(t *testing.T) {
	cfg, err := LoadFromBytes([]byte("[app]\nid='cli_x'\nsecret='s_x'\n"))
	if err != nil {
		t.Fatalf("LoadFromBytes returned error: %v", err)
	}

	if cfg.MCP.Endpoint == "" {
		t.Fatal("expected mcp endpoint default")
	}
	if cfg.MCP.TimeoutSec <= 0 {
		t.Fatalf("expected positive timeout, got %d", cfg.MCP.TimeoutSec)
	}
	if cfg.Commands.HeartbeatSec <= 0 {
		t.Fatalf("expected positive heartbeat, got %d", cfg.Commands.HeartbeatSec)
	}
	if cfg.Commands.StartReaction == "" {
		t.Fatal("expected start_reaction default")
	}
	if cfg.Runtime.TopicStateFile == "" {
		t.Fatal("expected topic_state_file default")
	}
	if cfg.Logging.Format != "text" {
		t.Fatalf("expected default logging format text, got %q", cfg.Logging.Format)
	}
	if cfg.Logging.Path != "logs/frieren.log" {
		t.Fatalf("expected default logging path logs/frieren.log, got %q", cfg.Logging.Path)
	}
}

func TestLoadFromBytes_RejectsEmptyProjectCWD(t *testing.T) {
	_, err := LoadFromBytes([]byte(`
[app]
id='cli_x'
secret='s_x'

[projects.bad]
cwd=''
`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "projects.bad.cwd") {
		t.Fatalf("unexpected error: %v", err)
	}
}
