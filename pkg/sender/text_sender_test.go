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

func TestSend_RequiresInputs(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api)

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
	s := NewTextSender(api)

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

func TestSend_UsesPostForShortPlainText(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api)

	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "处理完成"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(api.replyReqs) != 1 {
		t.Fatalf("expected one reply request, got %d", len(api.replyReqs))
	}
	if api.replyReqs[0].Body.MsgType == nil || *api.replyReqs[0].Body.MsgType != "post" {
		t.Fatalf("expected post msg type, got %+v", api.replyReqs[0].Body.MsgType)
	}
}

func TestSend_UsesTextForMarkdownAndCode(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api)

	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "```go\nfmt.Println(1)\n```"})
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
	s := NewTextSender(api)
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
	s := NewTextSender(&fakeMessageAPI{err: errors.New("boom")})
	_, err := s.Send(context.Background(), SendRequest{ChatID: "oc_x", ReplyToMessageID: "om_msg", Text: "hello"})
	if err == nil {
		t.Fatal("expected api error")
	}
}

func strPtr(v string) *string {
	return &v
}
