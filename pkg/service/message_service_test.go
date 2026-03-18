package service

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestFormatCodexOutput_RemovesStructuredPayloadAndKeepsMarkdown(t *testing.T) {
	content := "`DXF` is **distributed**.\n\n- [`pkg/dxf/framework/doc.go:17`](/Users/jujiajia/code/pingcap/tidb/pkg/dxf/framework/doc.go:17)"
	encodedPayload, err := json.MarshalIndent(map[string]string{
		"content":  content,
		"threadId": "codex_t1",
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	rawOutput := content + "\n" + string(encodedPayload)
	got := formatCodexOutput(rawOutput, "")

	if strings.Contains(got, `"content"`) {
		t.Fatalf("expected structured payload removed from output, got %q", got)
	}
	if !strings.Contains(got, "`DXF`") {
		t.Fatalf("expected inline markdown preserved, got %q", got)
	}
	if !strings.Contains(got, "**distributed**") {
		t.Fatalf("expected emphasis markdown preserved, got %q", got)
	}
	if !strings.Contains(got, "[`pkg/dxf/framework/doc.go:17`](/Users/jujiajia/code/pingcap/tidb/pkg/dxf/framework/doc.go:17)") {
		t.Fatalf("expected markdown link preserved, got %q", got)
	}
	if !strings.Contains(got, "线程信息：") {
		t.Fatalf("expected thread info section, got %q", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "codex_thread_id: codex_t1") {
		t.Fatalf("expected thread id at bottom of output, got %q", got)
	}
}

func TestExtractCodexStructuredPayload_WithoutSuffixJSON(t *testing.T) {
	content, threadID, ok := extractCodexStructuredPayload("plain output without json")
	if ok {
		t.Fatalf("expected no structured payload, got content=%q threadID=%q", content, threadID)
	}
}
