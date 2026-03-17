package service

import "testing"

func TestStripMentions(t *testing.T) {
	got := stripMentions("<at user_id=\"ou_bot\"></at>   /help")
	if got != "/help" {
		t.Fatalf("unexpected stripped text: %q", got)
	}
}

func TestParseProjectCommand(t *testing.T) {
	alias, prompt, ok := parseProjectCommand("/tidb 修复测试")
	if !ok {
		t.Fatal("expected project command")
	}
	if alias != "tidb" || prompt != "修复测试" {
		t.Fatalf("unexpected parse result: alias=%q prompt=%q", alias, prompt)
	}
}
