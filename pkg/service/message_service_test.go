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

func TestParseProjectCommand_AllowsMultilinePrompt(t *testing.T) {
	input := "/tidb first line\nsecond line"
	alias, prompt, ok := parseProjectCommand(input)
	if !ok {
		t.Fatal("expected project command for multiline prompt")
	}
	if alias != "tidb" || prompt != "first line\nsecond line" {
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
	got := formatCodexOutput(rawOutput, "", "")

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
	if !strings.Contains(got, "Thread info:") {
		t.Fatalf("expected thread info section, got %q", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "codex_thread_id: codex_t1") {
		t.Fatalf("expected thread id at bottom of output, got %q", got)
	}
}

func TestFormatCodexOutput_AppendsContextWindowUsageToFooter(t *testing.T) {
	got := formatCodexOutput("done", "codex_t1", "123K / 272K tokens used (55% left)")

	if !strings.Contains(got, "context_window: 123K / 272K tokens used (55% left)") {
		t.Fatalf("expected context window footer, got %q", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "codex_thread_id: codex_t1") {
		t.Fatalf("expected thread id to remain in footer, got %q", got)
	}
}

func TestParseCodexContextWindowUsage_FromStructuredJSON(t *testing.T) {
	raw := "status ok\n{\n  \"contextWindow\": {\n    \"usedTokens\": 123456,\n    \"maxTokens\": 272000\n  }\n}"

	usage, ok := parseCodexContextWindowUsage(raw)
	if !ok {
		t.Fatalf("expected usage parsed, got %#v", usage)
	}
	if usage.UsedTokens != 123456 || usage.MaxTokens != 272000 {
		t.Fatalf("unexpected usage parsed: %#v", usage)
	}
}

func TestFormatContextWindowUsage(t *testing.T) {
	got := formatContextWindowUsage(codexContextWindowUsage{
		UsedTokens: 123456,
		MaxTokens:  272000,
	})

	if got != "123K / 272K tokens used (55% left)" {
		t.Fatalf("unexpected usage text: %q", got)
	}
}

func TestExtractCodexStructuredPayload_WithoutSuffixJSON(t *testing.T) {
	content, threadID, ok := extractCodexStructuredPayload("plain output without json")
	if ok {
		t.Fatalf("expected no structured payload, got content=%q threadID=%q", content, threadID)
	}
}
