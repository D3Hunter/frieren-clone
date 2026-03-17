package config

import (
	"fmt"
	"strings"
)

const defaultReply = "收到，你的消息已处理。"

type Config struct {
	App      AppConfig      `toml:"app"`
	LongConn LongConnConfig `toml:"long_conn"`
	Message  MessageConfig  `toml:"message"`
	Logging  LoggingConfig  `toml:"logging"`
}

type AppConfig struct {
	ID     string `toml:"id"`
	Secret string `toml:"secret"`
}

type LongConnConfig struct {
	LogLevel            string `toml:"log_level"`
	AutoReconnect       bool   `toml:"auto_reconnect"`
	ReconnectBackoffSec int    `toml:"reconnect_backoff_sec"`
}

type MessageConfig struct {
	DefaultReply      string `toml:"default_reply"`
	EchoMode          bool   `toml:"echo_mode"`
	IgnoreBotMessages bool   `toml:"ignore_bot_messages"`
}

type LoggingConfig struct {
	Level string `toml:"level"`
}

func defaultConfig() Config {
	return Config{
		LongConn: LongConnConfig{
			LogLevel:            "info",
			AutoReconnect:       true,
			ReconnectBackoffSec: 3,
		},
		Message: MessageConfig{
			DefaultReply:      defaultReply,
			EchoMode:          true,
			IgnoreBotMessages: true,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.App.ID) == "" {
		return fmt.Errorf("app.id is required")
	}
	if strings.TrimSpace(c.App.Secret) == "" {
		return fmt.Errorf("app.secret is required")
	}

	if c.LongConn.ReconnectBackoffSec <= 0 {
		c.LongConn.ReconnectBackoffSec = 3
	}

	if c.LongConn.LogLevel == "" {
		c.LongConn.LogLevel = "info"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Message.DefaultReply == "" {
		c.Message.DefaultReply = defaultReply
	}

	return nil
}
