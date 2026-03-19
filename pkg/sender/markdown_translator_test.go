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
	if !strings.Contains(output, "# Report") {
		t.Fatalf("expected heading preserved, got %q", output)
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
