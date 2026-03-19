package sender

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeMessageAPI struct {
	createReqs []*larkim.CreateMessageReq
	replyReqs  []*larkim.ReplyMessageReq
	createResp *larkim.CreateMessageResp
	replyResp  *larkim.ReplyMessageResp
	err        error
	replyErrs  []error
}

func (f *fakeMessageAPI) Create(ctx context.Context, req *larkim.CreateMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageResp, error) {
	f.createReqs = append(f.createReqs, req)
	if f.err != nil {
		return nil, f.err
	}
	if f.createResp == nil {
		f.createResp = &larkim.CreateMessageResp{CodeError: larkcore.CodeError{Code: 0}}
	}
	return f.createResp, nil
}

func (f *fakeMessageAPI) Reply(ctx context.Context, req *larkim.ReplyMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.ReplyMessageResp, error) {
	f.replyReqs = append(f.replyReqs, req)
	if len(f.replyErrs) > 0 {
		nextErr := f.replyErrs[0]
		f.replyErrs = f.replyErrs[1:]
		if nextErr != nil {
			return nil, nextErr
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.replyResp == nil {
		f.replyResp = &larkim.ReplyMessageResp{CodeError: larkcore.CodeError{Code: 0}, Data: &larkim.ReplyMessageRespData{ThreadId: strPtr("omt_topic")}}
	}
	return f.replyResp, nil
}

type fakeReactionAPI struct {
	reactionReqs []*larkim.CreateMessageReactionReq
	reactionResp *larkim.CreateMessageReactionResp
	err          error
}

func (f *fakeReactionAPI) Create(ctx context.Context, req *larkim.CreateMessageReactionReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageReactionResp, error) {
	f.reactionReqs = append(f.reactionReqs, req)
	if f.err != nil {
		return nil, f.err
	}
	if f.reactionResp == nil {
		f.reactionResp = &larkim.CreateMessageReactionResp{CodeError: larkcore.CodeError{Code: 0}}
	}
	return f.reactionResp, nil
}

func TestSend_RequiresInputs(t *testing.T) {
	msgAPI := &fakeMessageAPI{}
	s := NewTextSender(msgAPI, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{Text: "hello"})
	if err == nil {
		t.Fatal("expected error for empty chat id")
	}
	_, err = s.Send(context.Background(), SendRequest{ChatID: "oc_x"})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSend_ReplyInThreadForTopicMessages(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "hello"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body == nil || api.replyReqs[0].Body.ReplyInThread == nil || !*api.replyReqs[0].Body.ReplyInThread {
		t.Fatal("expected reply_in_thread=true")
	}
}

func TestSend_UsesTextForShortPlainText(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "处理完成"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body.MsgType == nil || *api.replyReqs[0].Body.MsgType != "text" {
		t.Fatalf("expected text msg type, got %+v", api.replyReqs[0].Body.MsgType)
	}
}

func TestSend_CodexMarkdownModeUsesInteractiveCardMarkdown(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             "```go\nfmt.Println(1)\n```",
		RenderMode:       string(renderModeCodexMarkdown),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body.MsgType == nil || *api.replyReqs[0].Body.MsgType != "interactive" {
		t.Fatalf("expected interactive msg type, got %+v", api.replyReqs[0].Body.MsgType)
	}
	if api.replyReqs[0].Body.Content == nil {
		t.Fatal("expected card content")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(*api.replyReqs[0].Body.Content), &payload); err != nil {
		t.Fatalf("expected valid interactive payload: %v", err)
	}
	if got := payload["schema"]; got != "2.0" {
		t.Fatalf("expected schema 2.0, got %#v", got)
	}
	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body elements, got %#v", body["elements"])
	}
	first, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element object, got %#v", elements[0])
	}
	if got := first["tag"]; got != "markdown" {
		t.Fatalf("expected markdown element tag, got %#v", got)
	}
}

func TestBuildContent_InteractivePreservesMarkdownListMarkers(t *testing.T) {
	content, err := buildContent("interactive", "- first\n- second\n\n**bold**")
	if err != nil {
		t.Fatalf("buildContent error: %v", err)
	}
	if !strings.Contains(content, "- first") || !strings.Contains(content, "- second") {
		t.Fatalf("expected markdown list markers preserved, got %s", content)
	}
	if !strings.Contains(content, "**bold**") {
		t.Fatalf("expected markdown emphasis preserved, got %s", content)
	}
}

func TestSend_CodexMarkdownModeUnwrapsQuadrupleMarkdownFence(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	input := strings.Join([]string{
		"````markdown",
		"# Markdown Playground 2",
		"",
		"## Text Styles",
		"Use **bold** and `inline code`.",
		"",
		"```json",
		"{",
		`  "ok": true`,
		"}",
		"```",
		"````",
		"",
		"### Thread info",
		"- codex_thread_id: `tid_123`",
	}, "\n")

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             input,
		RenderMode:       string(renderModeCodexMarkdown),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body == nil || api.replyReqs[0].Body.Content == nil {
		t.Fatal("expected interactive card content")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(*api.replyReqs[0].Body.Content), &payload); err != nil {
		t.Fatalf("expected valid interactive payload: %v", err)
	}
	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body elements, got %#v", body["elements"])
	}
	first, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element object, got %#v", elements[0])
	}
	md, ok := first["content"].(string)
	if !ok {
		t.Fatalf("expected markdown string content, got %#v", first["content"])
	}
	if strings.Contains(md, "````markdown") {
		t.Fatalf("expected outer quadruple markdown fence removed, got %q", md)
	}
	if !strings.HasPrefix(strings.TrimSpace(md), "## Markdown Playground 2") {
		t.Fatalf("expected heading at top of translated markdown, got %q", md)
	}
}

func TestSend_CodexMarkdownModeKeepsPlaygroundHeadingForComplexPayload(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	input := strings.Join([]string{
		"````markdown",
		"# Markdown Playground 2",
		"",
		"## Text Styles",
		"This has **bold**, *italic*, ***bold+italic***, ~~strikethrough~~, and `inline code`.",
		"",
		"---",
		"",
		"## Links",
		"- Inline link: [TiDB repo](https://github.com/pingcap/tidb)",
		"- Auto-link: <https://example.com>",
		"- Footnote reference[^demo]",
		"",
		"## Task List",
		"- [x] Sample created",
		"- [ ] Add more cases",
		"",
		"## Collapsible Section",
		"<details>",
		"<summary>Expand me</summary>",
		"",
		"Inside the fold you can include text, lists, and code.",
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
		"",
		"## Image",
		"![Placeholder image](https://picsum.photos/360/120)",
		"",
		"[^demo]: Footnotes are supported in many Markdown renderers.",
		"````",
		"",
		"### Thread info",
		"- context_window: 17K / 258K tokens used (93% left)",
		"- codex_thread_id: `tid_123`",
	}, "\n")

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             input,
		RenderMode:       string(renderModeCodexMarkdown),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body == nil || api.replyReqs[0].Body.Content == nil {
		t.Fatal("expected interactive card content")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(*api.replyReqs[0].Body.Content), &payload); err != nil {
		t.Fatalf("expected valid interactive payload: %v", err)
	}
	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body elements, got %#v", body["elements"])
	}
	first, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element object, got %#v", elements[0])
	}
	md, ok := first["content"].(string)
	if !ok {
		t.Fatalf("expected markdown string content, got %#v", first["content"])
	}
	if strings.Contains(md, "````markdown") {
		t.Fatalf("expected outer quadruple markdown fence removed, got %q", md)
	}
	if !strings.HasPrefix(strings.TrimSpace(md), "## Markdown Playground 2") {
		t.Fatalf("expected heading at top of translated markdown, got %q", md)
	}
}

func TestSend_CodexMarkdownModeUsesSafeChunkSizeForInteractiveCards(t *testing.T) {
	const safeMarkdownChunkLimit = 1400

	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	input := "## Markdown Playground (2K+ Characters)\n\n" +
		strings.Repeat("Markdown rendering should remain readable even with long-form text. ", 90)

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             input,
		RenderMode:       string(renderModeCodexMarkdown),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) < 2 {
		t.Fatalf("expected long markdown to split into multiple chunks, got %d", len(api.replyReqs))
	}

	for i, req := range api.replyReqs {
		if req.Body == nil || req.Body.MsgType == nil || *req.Body.MsgType != "interactive" {
			t.Fatalf("chunk %d expected interactive message, got %+v", i, req.Body)
		}
		if req.Body.Content == nil {
			t.Fatalf("chunk %d expected interactive content", i)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(*req.Body.Content), &payload); err != nil {
			t.Fatalf("chunk %d invalid interactive payload: %v", i, err)
		}
		body, ok := payload["body"].(map[string]any)
		if !ok {
			t.Fatalf("chunk %d expected body object, got %#v", i, payload["body"])
		}
		elements, ok := body["elements"].([]any)
		if !ok || len(elements) == 0 {
			t.Fatalf("chunk %d expected non-empty body elements, got %#v", i, body["elements"])
		}
		md, ok := elements[0].(map[string]any)["content"].(string)
		if !ok {
			t.Fatalf("chunk %d expected markdown content string", i)
		}
		if got := utf8.RuneCountInString(md); got > safeMarkdownChunkLimit {
			t.Fatalf("chunk %d markdown content too large for safe interactive size: %d > %d", i, got, safeMarkdownChunkLimit)
		}
	}
}

func TestSend_CodexMarkdownModeMultiChunkKeepsHeadingAtChunkStart(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	input := "# Markdown Capability Demo (H1, Level 1)\n\n" +
		strings.Repeat("Markdown chunking should preserve heading rendering fidelity. ", 120)

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             input,
		RenderMode:       string(renderModeCodexMarkdown),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) < 2 {
		t.Fatalf("expected markdown response to split into multiple chunks, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body == nil || api.replyReqs[0].Body.Content == nil {
		t.Fatal("expected first chunk content")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(*api.replyReqs[0].Body.Content), &payload); err != nil {
		t.Fatalf("expected valid interactive payload: %v", err)
	}
	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body elements, got %#v", body["elements"])
	}
	md, ok := elements[0].(map[string]any)["content"].(string)
	if !ok {
		t.Fatalf("expected markdown content string, got %#v", elements[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(md), "## Markdown Capability Demo (H1, Level 1)") {
		t.Fatalf("expected first markdown chunk to start with heading, got %q", md)
	}
	if !strings.Contains(md, "[1/") {
		t.Fatalf("expected first chunk to include ordering marker, got %q", md)
	}
}

func TestSend_DefaultModeKeepsMarkdownAsPlainText(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             "1. first\n2. second\n\nplain tail",
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body.MsgType == nil || *api.replyReqs[0].Body.MsgType != "text" {
		t.Fatalf("expected text msg type, got %+v", api.replyReqs[0].Body.MsgType)
	}
}

func TestSend_SplitsLongOutputIntoOrderedChunks(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})
	s.SetMaxChunkRunesForTest(40)

	input := strings.Repeat("a", 120)
	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: input})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(api.replyReqs))
	}

	for i, req := range api.replyReqs {
		if req.Body == nil || req.Body.Content == nil {
			t.Fatalf("chunk %d missing content", i)
		}
		var content map[string]string
		if err := json.Unmarshal([]byte(*req.Body.Content), &content); err != nil {
			t.Fatalf("chunk %d invalid json content: %v", i, err)
		}
		if !strings.Contains(content["text"], "[") {
			t.Fatalf("chunk %d missing ordering prefix: %q", i, content["text"])
		}
	}
}

func TestSplitChunks_PrefersLineBoundaries(t *testing.T) {
	input := strings.Join([]string{
		"line-01 short",
		"line-02 short",
		"line-03 short",
		"line-04 short",
		"line-05 short",
	}, "\n")

	chunks := splitChunks(input, 36)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks[:len(chunks)-1] {
		if !strings.HasSuffix(chunk, "\n") {
			t.Fatalf("expected chunk %d to end at line boundary, got %q", i, chunk)
		}
	}
}

func TestSplitChunks_FallsBackToWordBoundariesForLongSingleLine(t *testing.T) {
	input := "alpha beta gamma delta epsilon zeta eta theta"
	chunks := splitChunks(input, 12)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	if strings.Join(chunks, "") != input {
		t.Fatalf("chunks should reconstruct original input, got %#v", chunks)
	}
	for i, chunk := range chunks {
		if len([]rune(chunk)) > 12 {
			t.Fatalf("chunk %d exceeds max runes: %q", i, chunk)
		}
		if i == len(chunks)-1 {
			continue
		}
		lastRune := []rune(chunk)[len([]rune(chunk))-1]
		if !lastRuneIsWhitespace(lastRune) {
			t.Fatalf("expected chunk %d to end at whitespace boundary, got %q", i, chunk)
		}
	}
}

func TestSplitMarkdownChunks_DoesNotSplitInsideFencedCodeBlock(t *testing.T) {
	input := strings.Join([]string{
		"intro",
		"",
		"```go",
		`fmt.Println("hello")`,
		`fmt.Println("world")`,
		"```",
		"",
		"tail " + strings.Repeat("x", 80),
	}, "\n")

	chunks := splitMarkdownChunks(input, 70)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.Count(chunk, "```")%2 != 0 {
			t.Fatalf("chunk %d contains unmatched code fence: %q", i, chunk)
		}
	}
}

func TestSplitMarkdownChunks_OversizedFencedBlockKeepsBalancedFencesPerChunk(t *testing.T) {
	bodyLines := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("line-%03d: %s", i, strings.Repeat("x", 18)))
	}
	input := strings.Join([]string{
		"```go",
		strings.Join(bodyLines, "\n"),
		"```",
	}, "\n")

	chunks := splitMarkdownChunks(input, 220)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized fenced block to split into multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.Count(chunk, "```")%2 != 0 {
			t.Fatalf("chunk %d contains unmatched code fence: %q", i, chunk)
		}
	}
}

func TestSplitMarkdownBlocks_FenceCloserRequiresWhitespaceSuffixOnly(t *testing.T) {
	input := strings.Join([]string{
		"```markdown",
		"# literal examples",
		"```json",
		`{"ok": true}`,
		"```",
		"after",
	}, "\n")

	blocks := splitMarkdownBlocks(input)
	if len(blocks) < 2 {
		t.Fatalf("expected fenced block and trailing paragraph blocks, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "```json\n{\"ok\": true}\n```") {
		t.Fatalf("expected fence content line with language suffix to remain inside fenced block, got %q", blocks[0])
	}
	if strings.Contains(blocks[0], "after") {
		t.Fatalf("expected trailing paragraph outside fenced block, got %q", blocks[0])
	}
}

func TestSplitMarkdownChunks_DoesNotSplitTableHeaderFromSeparator(t *testing.T) {
	input := strings.Join([]string{
		"# report",
		"",
		"| name | score |",
		"| --- | --- |",
		"| alpha | 1 |",
		"| beta | 2 |",
		"",
		"appendix " + strings.Repeat("tail ", 30),
	}, "\n")

	chunks := splitMarkdownChunks(input, 75)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.Contains(chunk, "| --- | --- |") && !strings.Contains(chunk, "| name | score |") {
			t.Fatalf("chunk %d split table header from separator: %q", i, chunk)
		}
	}
}

func TestSplitMarkdownChunks_KeepsSectionHeadingWithFollowingTableBlock(t *testing.T) {
	intro := strings.Repeat("Intro paragraph content to consume chunk budget. ", 12)
	input := strings.Join([]string{
		intro,
		"",
		"## Table Alignment Test",
		"",
		"| Left align | Center align | Right align |",
		"|:---|:---:|---:|",
		"| apple | red | 10 |",
		"| banana | yellow | 200 |",
		"| cherry | dark red | 3000 |",
		"",
		"tail",
	}, "\n")

	chunks := splitMarkdownChunks(input, 680)
	if len(chunks) < 2 {
		t.Fatalf("expected split into multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		hasTable := strings.Contains(chunk, "| Left align | Center align | Right align |") ||
			strings.Contains(chunk, "|:---|:---:|---:|")
		if !hasTable {
			continue
		}
		if !strings.Contains(chunk, "## Table Alignment Test") {
			t.Fatalf("expected table chunk %d to include its section heading, got %q", i, chunk)
		}
	}
}

func TestSend_CodexMarkdownModeFallsBackToPlainTextWhenCardFails(t *testing.T) {
	api := &fakeMessageAPI{
		replyErrs: []error{errors.New("interactive card failed"), nil},
	}
	s := NewTextSender(api, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{
		ChatID:           "oc_x",
		ReplyToMessageID: "om_msg",
		Text:             "**bold**",
		RenderMode:       string(renderModeCodexMarkdown),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 2 {
		t.Fatalf("expected interactive attempt + plain-text fallback, got %d requests", len(api.replyReqs))
	}
	if got := *api.replyReqs[0].Body.MsgType; got != "interactive" {
		t.Fatalf("first attempt should be interactive, got %q", got)
	}
	if got := *api.replyReqs[1].Body.MsgType; got != "text" {
		t.Fatalf("fallback should use text, got %q", got)
	}
}

func lastRuneIsWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

func TestSend_PropagatesAPIError(t *testing.T) {
	s := NewTextSender(&fakeMessageAPI{err: errors.New("boom")}, &fakeReactionAPI{err: errors.New("boom")})
	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "hello"})
	if err == nil {
		t.Fatal("expected api error")
	}
}

func TestAddReaction_UsesMessageReactionAPI(t *testing.T) {
	reactionAPI := &fakeReactionAPI{}
	s := NewTextSender(&fakeMessageAPI{}, reactionAPI)

	if err := s.AddReaction(context.Background(), AddReactionRequest{MessageID: "om_msg", EmojiType: "OnIt"}); err != nil {
		t.Fatalf("AddReaction error: %v", err)
	}
	if len(reactionAPI.reactionReqs) != 1 {
		t.Fatalf("expected one reaction request, got %d", len(reactionAPI.reactionReqs))
	}
	if reactionAPI.reactionReqs[0].Body == nil || reactionAPI.reactionReqs[0].Body.ReactionType == nil || reactionAPI.reactionReqs[0].Body.ReactionType.EmojiType == nil {
		t.Fatalf("expected reaction body with emoji type, got %+v", reactionAPI.reactionReqs[0].Body)
	}
	if *reactionAPI.reactionReqs[0].Body.ReactionType.EmojiType != "OnIt" {
		t.Fatalf("expected OnIt emoji type, got %q", *reactionAPI.reactionReqs[0].Body.ReactionType.EmojiType)
	}
}

func strPtr(v string) *string {
	return &v
}
