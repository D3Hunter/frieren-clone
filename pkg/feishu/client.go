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
	"go.uber.org/zap"
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

func NewAppClient(cfg config.Config, logger *zap.Logger) *lark.Client {
	level := ToLogLevel(cfg.Logging.Level)
	options := []lark.ClientOptionFunc{
		lark.WithLogLevel(level),
	}
	if logger != nil {
		options = append(options, lark.WithLogger(newLarkZapLogger(logger.Named("feishu.http"), cfg.App.Secret)))
	}
	return lark.NewClient(
		cfg.App.ID,
		cfg.App.Secret,
		options...,
	)
}

func NewWSClient(cfg config.Config, messageHandler func(context.Context, *larkim.P2MessageReceiveV1) error, logger *zap.Logger) *larkws.Client {
	dispatcher := larkevent.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(messageHandler)

	options := []larkws.ClientOption{
		larkws.WithEventHandler(dispatcher),
		larkws.WithLogLevel(ToLogLevel(cfg.LongConn.LogLevel)),
		larkws.WithAutoReconnect(cfg.LongConn.AutoReconnect),
	}
	if logger != nil {
		options = append(options, larkws.WithLogger(newLarkZapLogger(logger.Named("feishu.ws"), cfg.App.Secret)))
	}

	return larkws.NewClient(
		cfg.App.ID,
		cfg.App.Secret,
		options...,
	)
}
