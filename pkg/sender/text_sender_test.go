package sender

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeMessageAPI struct {
	createReqs []*larkim.CreateMessageReq
	replyReqs  []*larkim.ReplyMessageReq
	createResp *larkim.CreateMessageResp
	replyResp  *larkim.ReplyMessageResp
	err        error
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

func TestSend_UsesInteractiveForMarkdownAndCode(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api, &fakeReactionAPI{})

	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "```go\nfmt.Println(1)\n```"})
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
	if !strings.Contains(*api.replyReqs[0].Body.Content, "\"tag\":\"lark_md\"") {
		t.Fatalf("expected lark_md card content, got %s", *api.replyReqs[0].Body.Content)
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
