package feishumarkdown

import (
	"regexp"
	"strings"
	"testing"
)

func TestTranslateCodexMarkdownToFeishu_PreservesCoreMarkdownStructures(t *testing.T) {
	input := strings.Join([]string{
		"# Report",
		"",
		"> status is **good**",
		"",
		"1. alpha",
		"2. beta",
		"",
		"```go",
		`fmt.Println("ok")`,
		"```",
		"",
		"| name | score |",
		"| --- | --- |",
		"| a | 1 |",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if !strings.Contains(output, "## Report") {
		t.Fatalf("expected h1 downgraded to feishu-compatible heading level, got %q", output)
	}
	if !strings.Contains(output, "> status is **good**") {
		t.Fatalf("expected blockquote + emphasis preserved, got %q", output)
	}
	if !strings.Contains(output, "1. alpha") || !strings.Contains(output, "2. beta") {
		t.Fatalf("expected ordered list preserved, got %q", output)
	}
	if !strings.Contains(output, "```go") || !strings.Contains(output, `fmt.Println("ok")`) {
		t.Fatalf("expected fenced code block preserved, got %q", output)
	}
	if !strings.Contains(output, "| name | score |") || !strings.Contains(output, "| --- | --- |") {
		t.Fatalf("expected table preserved, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_DegradesLocalLinksAndImagesForCompatibility(t *testing.T) {
	input := strings.Join([]string{
		"[Local](/Users/jujiajia/code/frieren-clone/pkg/service/message_service.go)",
		"[Web](https://example.com/path)",
		"![Architecture](https://example.com/arch.png)",
		"![Diagram](/tmp/diagram.png)",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if !strings.Contains(output, "`/Users/jujiajia/code/frieren-clone/pkg/service/message_service.go`") {
		t.Fatalf("expected local path rendered as inline code, got %q", output)
	}
	if strings.Contains(output, "](/Users/jujiajia/code/frieren-clone/pkg/service/message_service.go)") {
		t.Fatalf("expected local file markdown link removed, got %q", output)
	}
	if !strings.Contains(output, "[Web](https://example.com/path)") {
		t.Fatalf("expected https link preserved, got %q", output)
	}
	if strings.Contains(output, "![Architecture](") {
		t.Fatalf("expected markdown image syntax degraded for feishu compatibility, got %q", output)
	}
	if !strings.Contains(output, "[Architecture](https://example.com/arch.png)") {
		t.Fatalf("expected http image converted to link, got %q", output)
	}
	if !strings.Contains(output, "Diagram (`/tmp/diagram.png`)") {
		t.Fatalf("expected local image path degraded to readable text, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_EscapesRawHTMLAndRendersTaskLists(t *testing.T) {
	input := strings.Join([]string{
		"<div>alpha</div>",
		"",
		"inline <span>beta</span>",
		"",
		"- [x] done",
		"- [ ] todo",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if strings.Contains(output, "<div>") || strings.Contains(output, "<span>") {
		t.Fatalf("expected raw html escaped, got %q", output)
	}
	if !strings.Contains(output, "&lt;div&gt;alpha&lt;/div&gt;") {
		t.Fatalf("expected escaped html block, got %q", output)
	}
	if !strings.Contains(output, "&lt;span&gt;beta&lt;/span&gt;") {
		t.Fatalf("expected escaped inline html, got %q", output)
	}
	if !strings.Contains(output, "[x] done") {
		t.Fatalf("expected checked task marker, got %q", output)
	}
	if !strings.Contains(output, "[ ] todo") {
		t.Fatalf("expected unchecked task marker, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_NormalizesNestedListIndentation(t *testing.T) {
	input := "- parent\n    - child\n        - grandchild"

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if !regexp.MustCompile(`(?m)^ {2}- child$`).MatchString(output) {
		t.Fatalf("expected nested list indented by 2 spaces, got %q", output)
	}
	if !regexp.MustCompile(`(?m)^ {4}- grandchild$`).MatchString(output) {
		t.Fatalf("expected deeper nested list indented by 4 spaces, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_UnwrapsTopLevelMarkdownFence(t *testing.T) {
	input := strings.Join([]string{
		"```markdown",
		"# Markdown Test",
		"",
		"## List Example",
		"",
		"- First bullet",
		"- Second bullet",
		"",
		"## Numbered Steps",
		"",
		"1. Open the file",
		"2. Edit the content",
		"```",
		"",
		"Thread info:",
		"codex_thread_id: tid_123",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if strings.Contains(output, "```markdown") {
		t.Fatalf("expected top-level markdown fence to be unwrapped, got %q", output)
	}
	if !strings.Contains(output, "## Markdown Test") {
		t.Fatalf("expected top heading downgraded for feishu compatibility, got %q", output)
	}
	if !strings.Contains(output, "## List Example") {
		t.Fatalf("expected sub-heading preserved after unwrap, got %q", output)
	}
	if !strings.Contains(output, "- First bullet") || !strings.Contains(output, "1. Open the file") {
		t.Fatalf("expected list items preserved after unwrap, got %q", output)
	}
	if !strings.Contains(output, "Thread info:\ncodex_thread_id: tid_123") {
		t.Fatalf("expected trailing thread footer preserved, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_UnwrapsLargeInnerMarkdownFence(t *testing.T) {
	input := strings.Join([]string{
		"Intro paragraph before a wrapped markdown block.",
		"",
		"```markdown",
		"# Wrapped Markdown Title",
		"",
		"## Wrapped Section",
		"",
		"- item one",
		"- item two",
		"",
		"```bash",
		"echo wrapped",
		"```",
		"",
		"| Name | Value |",
		"| --- | --- |",
		"| alpha | 1 |",
		"",
		strings.Repeat("long wrapped markdown paragraph for render testing. ", 30),
		"```",
		"",
		"Footer after wrapped markdown block.",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if strings.Contains(output, "```markdown") {
		t.Fatalf("expected inner markdown wrapper removed, got %q", output)
	}
	if !strings.Contains(output, "## Wrapped Markdown Title") {
		t.Fatalf("expected wrapped heading rendered as markdown heading, got %q", output)
	}
	if !strings.Contains(output, "```bash\necho wrapped\n```") {
		t.Fatalf("expected nested fenced code block preserved, got %q", output)
	}
	if !strings.Contains(output, "| Name | Value |") || !strings.Contains(output, "| alpha | 1 |") {
		t.Fatalf("expected wrapped table rendered, got %q", output)
	}
	if !strings.Contains(output, "Footer after wrapped markdown block.") {
		t.Fatalf("expected footer text preserved, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_KeepsSmallMarkdownFenceAsCode(t *testing.T) {
	input := strings.Join([]string{
		"Code sample:",
		"",
		"```markdown",
		"# tiny",
		"- item",
		"```",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}
	if !strings.Contains(output, "```markdown") {
		t.Fatalf("expected small markdown fence to remain code block, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_TableRowsDoNotContainDanglingBackticks(t *testing.T) {
	input := strings.Join([]string{
		"| Feature | Syntax Example | Supported in GFM | Notes |",
		"| --- | --- | --- | --- |",
		"| Table | `| a | b |` | Yes | GitHub flavored |",
	}, "\n")

	output, err := TranslateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("TranslateCodexMarkdownToFeishu error: %v", err)
	}

	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		if countUnescapedBackticks(line)%2 != 0 {
			t.Fatalf("expected table row to avoid dangling backticks, got %q", line)
		}
	}
}

func TestRenderInlineCode_UsesFenceLongerThanContainedBackticks(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no backticks uses single fence",
			input:    "alpha",
			expected: "`alpha`",
		},
		{
			name:     "single backtick uses double fence",
			input:    "alpha`beta",
			expected: "``alpha`beta``",
		},
		{
			name:     "double backticks use triple fence",
			input:    "alpha``beta",
			expected: "```alpha``beta```",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderInlineCode(tc.input); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func countUnescapedBackticks(value string) int {
	count := 0
	escaped := false
	for _, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '`' {
			count++
		}
	}
	return count
}
