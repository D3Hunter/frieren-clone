package sender

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

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
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

func TestTranslateCodexMarkdownToFeishu_DowngradesH1ToH2ForFeishuCompatibility(t *testing.T) {
	output, err := translateCodexMarkdownToFeishu("# Top Title")
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
	}
	if regexp.MustCompile(`(?m)^# Top Title$`).MatchString(output) {
		t.Fatalf("expected h1 to be downgraded for feishu compatibility, got %q", output)
	}
	if !strings.Contains(output, "## Top Title") {
		t.Fatalf("expected downgraded h2 heading, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_DegradesLocalFileLinksToInlineCode(t *testing.T) {
	input := strings.Join([]string{
		"[Local](/Users/jujiajia/code/frieren-clone/pkg/service/message_service.go)",
		"[Web](https://example.com/path)",
	}, "\n")

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
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
}

func TestTranslateCodexMarkdownToFeishu_EscapesRawHTML(t *testing.T) {
	input := "<div>alpha</div>\n\ninline <span>beta</span>"
	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
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
}

func TestTranslateCodexMarkdownToFeishu_ConvertsTaskListMarkers(t *testing.T) {
	input := "- [x] done\n- [ ] todo"
	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
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
	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
	}
	re := regexp.MustCompile(`(?m)^ {2}- child$`)
	if !re.MatchString(output) {
		t.Fatalf("expected nested list indented by 2 spaces, got %q", output)
	}
	re = regexp.MustCompile(`(?m)^ {4}- grandchild$`)
	if !re.MatchString(output) {
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

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
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

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
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

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
	}
	if !strings.Contains(output, "```markdown") {
		t.Fatalf("expected small markdown fence to remain code block, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_UnwrapsMarkdownFenceWithNestedCodeAndPreservesFollowingBlocks(t *testing.T) {
	input := strings.Join([]string{
		"```markdown",
		"# Sample Markdown Output",
		"",
		"### Code Block",
		"```sql",
		"SELECT tidb_version();",
		"```",
		"",
		"### Table",
		"| Item | Status |",
		"| --- | --- |",
		"| Parser | ✅ |",
		"",
		"### Task List",
		"- [x] Create sample",
		"- [ ] Add more cases",
		"```",
		"",
		"Thread info:",
		"context_window: 17K / 258K tokens used (93% left)",
		"codex_thread_id: tid_123",
	}, "\n")

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
	}
	if strings.Contains(output, "```markdown") {
		t.Fatalf("expected top-level markdown fence removed, got %q", output)
	}
	if strings.Contains(output, "```\n\nThread info:") {
		t.Fatalf("expected no leaked wrapper fence before thread footer, got %q", output)
	}
	if !strings.Contains(output, "```sql\nSELECT tidb_version();\n```") {
		t.Fatalf("expected sql code block preserved as standalone block, got %q", output)
	}
	if !strings.Contains(output, "| Item | Status |") || !strings.Contains(output, "| Parser | ✅ |") {
		t.Fatalf("expected table block preserved after code block, got %q", output)
	}
	if !strings.Contains(output, "- [x] Create sample") || !strings.Contains(output, "- [ ] Add more cases") {
		t.Fatalf("expected task list markers preserved after table, got %q", output)
	}
	if !strings.Contains(output, "Thread info:\ncontext_window: 17K / 258K tokens used (93% left)\ncodex_thread_id: tid_123") {
		t.Fatalf("expected thread footer preserved after markdown blocks, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_UnwrapsQuadrupleFenceAndPreservesTopHeading(t *testing.T) {
	input := strings.Join([]string{
		"````markdown",
		"# Markdown Playground 2",
		"",
		"## Text Styles",
		"This has **bold**, *italic*, ***bold+italic***, ~~strikethrough~~, and `inline code`.",
		"",
		"## Collapsible Section",
		"<details>",
		"<summary>Expand me</summary>",
		"",
		"```json",
		"{",
		`  "kind": "markdown-demo",`,
		`  "ok": true`,
		"}",
		"```",
		"</details>",
		"",
		"## Table",
		"| Item | Align Left | Align Center | Align Right |",
		"|:-----|:-----------|:------------:|------------:|",
		"| A    | yes        | yes          | yes         |",
		"| B    | demo       | demo         | 123         |",
		"````",
		"",
		"### Thread info",
		"- context_window: 17K / 258K tokens used (93% left)",
		"- codex_thread_id: `tid_123`",
	}, "\n")

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
	}
	if strings.Contains(output, "````markdown") {
		t.Fatalf("expected quadruple markdown wrapper removed, got %q", output)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "## Markdown Playground 2") {
		t.Fatalf("expected top heading to remain a markdown heading, got %q", output)
	}
	if !strings.Contains(output, "### Thread info") {
		t.Fatalf("expected thread info section preserved, got %q", output)
	}
}

func TestTranslateCodexMarkdownToFeishu_TableRowsDoNotContainDanglingBackticks(t *testing.T) {
	input := strings.Join([]string{
		"| Feature | Syntax Example | Supported in GFM | Notes |",
		"| --- | --- | --- | --- |",
		"| Table | `| a | b |` | Yes | GitHub flavored |",
	}, "\n")

	output, err := translateCodexMarkdownToFeishu(input)
	if err != nil {
		t.Fatalf("translateCodexMarkdownToFeishu error: %v", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
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
