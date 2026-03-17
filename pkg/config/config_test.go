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

[runtime]
topic_state_file='./tmp/topic-state.json'

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
	if cfg.Runtime.TopicStateFile != "./tmp/topic-state.json" {
		t.Fatalf("unexpected topic state file: %q", cfg.Runtime.TopicStateFile)
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
	if cfg.Runtime.TopicStateFile == "" {
		t.Fatal("expected topic_state_file default")
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
