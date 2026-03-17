package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const defaultReply = "收到，你的消息已处理。"
const (
	defaultMCPEndpoint   = "http://localhost:8787/mcp"
	defaultMCPTimeoutSec = 30
	defaultHeartbeatSec  = 180
	defaultStartReaction = "OnIt"
)

type Config struct {
	App      AppConfig                `toml:"app"`
	LongConn LongConnConfig           `toml:"long_conn"`
	Message  MessageConfig            `toml:"message"`
	Logging  LoggingConfig            `toml:"logging"`
	MCP      MCPConfig                `toml:"mcp"`
	Commands CommandsConfig           `toml:"commands"`
	Runtime  RuntimeConfig            `toml:"runtime"`
	Projects map[string]ProjectConfig `toml:"projects"`
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
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type MCPConfig struct {
	Endpoint   string `toml:"endpoint"`
	TimeoutSec int    `toml:"timeout_sec"`
}

type CommandsConfig struct {
	BotOpenID     string `toml:"bot_open_id"`
	HeartbeatSec  int    `toml:"heartbeat_sec"`
	StartReaction string `toml:"start_reaction"`
}

type RuntimeConfig struct {
	TopicStateFile string `toml:"topic_state_file"`
}

type ProjectConfig struct {
	CWD string `toml:"cwd"`
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
			Level:  "info",
			Format: "text",
		},
		MCP: MCPConfig{
			Endpoint:   defaultMCPEndpoint,
			TimeoutSec: defaultMCPTimeoutSec,
		},
		Commands: CommandsConfig{
			HeartbeatSec:  defaultHeartbeatSec,
			StartReaction: defaultStartReaction,
		},
		Runtime: RuntimeConfig{
			TopicStateFile: filepath.Join(".state", "topic-state.json"),
		},
		Projects: map[string]ProjectConfig{},
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
	c.Logging.Format = strings.ToLower(strings.TrimSpace(c.Logging.Format))
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	if c.Message.DefaultReply == "" {
		c.Message.DefaultReply = defaultReply
	}
	if strings.TrimSpace(c.MCP.Endpoint) == "" {
		c.MCP.Endpoint = defaultMCPEndpoint
	}
	if c.MCP.TimeoutSec <= 0 {
		c.MCP.TimeoutSec = defaultMCPTimeoutSec
	}
	if c.Commands.HeartbeatSec <= 0 {
		c.Commands.HeartbeatSec = defaultHeartbeatSec
	}
	c.Commands.StartReaction = strings.TrimSpace(c.Commands.StartReaction)
	if c.Commands.StartReaction == "" {
		c.Commands.StartReaction = defaultStartReaction
	}
	if strings.TrimSpace(c.Runtime.TopicStateFile) == "" {
		c.Runtime.TopicStateFile = filepath.Join(".state", "topic-state.json")
	}
	if c.Projects == nil {
		c.Projects = map[string]ProjectConfig{}
	}
	normalizedProjects := make(map[string]ProjectConfig, len(c.Projects))
	for alias, project := range c.Projects {
		normalizedAlias := strings.ToLower(strings.TrimSpace(alias))
		if normalizedAlias == "" {
			return fmt.Errorf("projects alias is required")
		}
		project.CWD = strings.TrimSpace(project.CWD)
		if project.CWD == "" {
			return fmt.Errorf("projects.%s.cwd is required", normalizedAlias)
		}
		normalizedProjects[normalizedAlias] = project
	}
	c.Projects = normalizedProjects

	return nil
}
