package feishu

import (
	"context"
	"strings"

	"github.com/D3Hunter/frieren-clone/pkg/config"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func ToLogLevel(level string) larkcore.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return larkcore.LogLevelDebug
	case "warn":
		return larkcore.LogLevelWarn
	case "error":
		return larkcore.LogLevelError
	default:
		return larkcore.LogLevelInfo
	}
}

func NewAppClient(cfg config.Config) *lark.Client {
	level := ToLogLevel(cfg.Logging.Level)
	return lark.NewClient(
		cfg.App.ID,
		cfg.App.Secret,
		lark.WithLogLevel(level),
	)
}

func NewWSClient(cfg config.Config, messageHandler func(context.Context, *larkim.P2MessageReceiveV1) error) *larkws.Client {
	dispatcher := larkevent.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(messageHandler)

	return larkws.NewClient(
		cfg.App.ID,
		cfg.App.Secret,
		larkws.WithEventHandler(dispatcher),
		larkws.WithLogLevel(ToLogLevel(cfg.LongConn.LogLevel)),
		larkws.WithAutoReconnect(cfg.LongConn.AutoReconnect),
	)
}
