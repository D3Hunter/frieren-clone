package sender

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeMessageAPI struct {
	lastReq  *larkim.CreateMessageReq
	lastOpts []larkcore.RequestOptionFunc
	resp     *larkim.CreateMessageResp
	err      error
}

func (f *fakeMessageAPI) Create(ctx context.Context, req *larkim.CreateMessageReq, opts ...larkcore.RequestOptionFunc) (*larkim.CreateMessageResp, error) {
	f.lastReq = req
	f.lastOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	if f.resp == nil {
		f.resp = &larkim.CreateMessageResp{}
	}
	return f.resp, nil
}

func TestSendText_RequiresInputs(t *testing.T) {
	api := &fakeMessageAPI{}
	s := NewTextSender(api)

	if err := s.SendText(context.Background(), "", "hello"); err == nil {
		t.Fatal("expected error for empty chat id")
	}
	if err := s.SendText(context.Background(), "oc_x", ""); err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSendText_BuildsTextMessageBody(t *testing.T) {
	api := &fakeMessageAPI{resp: &larkim.CreateMessageResp{CodeError: larkcore.CodeError{Code: 0}}}
	s := NewTextSender(api)

	if err := s.SendText(context.Background(), "oc_x", "hello"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	if api.lastReq == nil || api.lastReq.Body == nil {
		t.Fatal("expected CreateMessageReq body to be set")
	}
	if api.lastReq.Body.ReceiveId == nil || *api.lastReq.Body.ReceiveId != "oc_x" {
		t.Fatalf("unexpected receive id: %+v", api.lastReq.Body.ReceiveId)
	}
	if api.lastReq.Body.MsgType == nil || *api.lastReq.Body.MsgType != "text" {
		t.Fatalf("unexpected msg type: %+v", api.lastReq.Body.MsgType)
	}
	if api.lastReq.Body.Content == nil {
		t.Fatal("expected content")
	}

	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*api.lastReq.Body.Content), &content); err != nil {
		t.Fatalf("content is not valid json: %v", err)
	}
	if content.Text != "hello" {
		t.Fatalf("unexpected content text: %q", content.Text)
	}
}

func TestSendText_PropagatesErrors(t *testing.T) {
	t.Run("request error", func(t *testing.T) {
		s := NewTextSender(&fakeMessageAPI{err: errors.New("boom")})
		if err := s.SendText(context.Background(), "oc_x", "hello"); err == nil {
			t.Fatal("expected request error")
		}
	})

	t.Run("api code error", func(t *testing.T) {
		s := NewTextSender(&fakeMessageAPI{resp: &larkim.CreateMessageResp{CodeError: larkcore.CodeError{Code: 90001, Msg: "failed"}}})
		if err := s.SendText(context.Background(), "oc_x", "hello"); err == nil {
			t.Fatal("expected api code error")
		}
	})
}
