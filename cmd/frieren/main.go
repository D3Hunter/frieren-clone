package main

import (
	"context"
	"flag"
	"log"

	"github.com/D3Hunter/frieren-clone/pkg/config"
	"github.com/D3Hunter/frieren-clone/pkg/feishu"
	"github.com/D3Hunter/frieren-clone/pkg/handler"
	"github.com/D3Hunter/frieren-clone/pkg/sender"
	"github.com/D3Hunter/frieren-clone/pkg/service"
)

func main() {
	configPath := flag.String("config", "example.toml", "path to toml config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	appClient := feishu.NewAppClient(*cfg)
	textSender := sender.NewTextSender(appClient.Im.V1.Message)
	processor := service.Processor{
		EchoMode:     cfg.Message.EchoMode,
		DefaultReply: cfg.Message.DefaultReply,
	}
	messageHandler := handler.NewMessageHandler(processor, textSender, cfg.Message.IgnoreBotMessages)

	wsClient := feishu.NewWSClient(*cfg, messageHandler.HandleEvent)

	log.Printf("starting long connection client")
	if err := wsClient.Start(context.Background()); err != nil {
		log.Fatalf("start long connection client: %v", err)
	}
}
