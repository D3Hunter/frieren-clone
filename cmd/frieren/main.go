package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/D3Hunter/frieren-clone/pkg/codex"
	"github.com/D3Hunter/frieren-clone/pkg/config"
	"github.com/D3Hunter/frieren-clone/pkg/feishu"
	"github.com/D3Hunter/frieren-clone/pkg/handler"
	"github.com/D3Hunter/frieren-clone/pkg/mcp"
	"github.com/D3Hunter/frieren-clone/pkg/runtime"
	"github.com/D3Hunter/frieren-clone/pkg/sender"
	"github.com/D3Hunter/frieren-clone/pkg/service"
)

type topicStoreAdapter struct {
	store *runtime.TopicStateStore
}

type mcpGatewayAdapter struct {
	gateway *mcp.Gateway
}

func (a mcpGatewayAdapter) ListTools(ctx context.Context) ([]service.ToolInfo, error) {
	tools, err := a.gateway.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		out = append(out, service.ToolInfo{Name: tool.Name, Description: tool.Description})
	}
	return out, nil
}

func (a mcpGatewayAdapter) GetToolSchema(ctx context.Context, tool string) (string, error) {
	return a.gateway.GetToolSchema(ctx, tool)
}

func (a mcpGatewayAdapter) CallTool(ctx context.Context, tool string, args map[string]any) (string, error) {
	return a.gateway.CallTool(ctx, tool, args)
}

type messageSenderAdapter struct {
	sender *sender.TextSender
}

func (a messageSenderAdapter) Send(ctx context.Context, msg service.OutgoingMessage) (service.SendReceipt, error) {
	receipt, err := a.sender.Send(ctx, sender.SendRequest{
		ChatID:           msg.ChatID,
		ReplyToMessageID: msg.ReplyToMessageID,
		ThreadID:         msg.ThreadID,
		Text:             msg.Text,
	})
	if err != nil {
		return service.SendReceipt{}, err
	}
	return service.SendReceipt{ThreadID: receipt.ThreadID}, nil
}

func (a messageSenderAdapter) AddReaction(ctx context.Context, reaction service.OutgoingReaction) error {
	return a.sender.AddReaction(ctx, sender.AddReactionRequest{
		MessageID: reaction.MessageID,
		EmojiType: reaction.EmojiType,
	})
}

func (a topicStoreAdapter) Get(chatID, feishuThreadID string) (service.TopicBinding, bool) {
	state, ok := a.store.Get(chatID, feishuThreadID)
	if !ok {
		return service.TopicBinding{}, false
	}
	return service.TopicBinding{
		ChatID:         state.ChatID,
		FeishuThreadID: state.FeishuThreadID,
		ProjectAlias:   state.ProjectAlias,
		CodexThreadID:  state.CodexThreadID,
	}, true
}

func (a topicStoreAdapter) Upsert(binding service.TopicBinding) error {
	return a.store.Upsert(runtime.TopicState{
		ChatID:         binding.ChatID,
		FeishuThreadID: binding.FeishuThreadID,
		ProjectAlias:   binding.ProjectAlias,
		CodexThreadID:  binding.CodexThreadID,
	})
}

func main() {
	configPath := flag.String("config", "example.toml", "path to toml config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	appClient := feishu.NewAppClient(*cfg)
	textSender := sender.NewTextSender(appClient.Im.V1.Message, appClient.Im.V1.MessageReaction)
	topicStore, err := runtime.NewTopicStateStore(cfg.Runtime.TopicStateFile)
	if err != nil {
		log.Fatalf("init topic state store: %v", err)
	}
	projectAliasCWD := make(map[string]string, len(cfg.Projects))
	for alias, project := range cfg.Projects {
		projectAliasCWD[alias] = project.CWD
	}
	commandService := service.NewCommandService(service.CommandServiceDeps{
		MCP:        mcpGatewayAdapter{gateway: mcp.NewGateway(cfg.MCP.Endpoint, time.Duration(cfg.MCP.TimeoutSec)*time.Second)},
		Codex:      codex.NewRunner(),
		Sender:     messageSenderAdapter{sender: textSender},
		TopicStore: topicStoreAdapter{store: topicStore},
		Config: service.CommandServiceConfig{
			BotOpenID:               cfg.Commands.BotOpenID,
			Heartbeat:               time.Duration(cfg.Commands.HeartbeatSec) * time.Second,
			StartProcessingReaction: cfg.Commands.StartReaction,
			ProjectAliasCWD:         projectAliasCWD,
		},
	})
	messageHandler := handler.NewMessageHandler(commandService, cfg.Message.IgnoreBotMessages)

	wsClient := feishu.NewWSClient(*cfg, messageHandler.HandleEvent)

	log.Printf("starting long connection client")
	if err := wsClient.Start(context.Background()); err != nil {
		log.Fatalf("start long connection client: %v", err)
	}
}
