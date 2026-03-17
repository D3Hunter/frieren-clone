package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Load reads a TOML file from disk, decodes it, and returns a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg, err := LoadFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, nil
}

// LoadFromBytes decodes TOML bytes into Config, applies defaults, and validates required fields.
func LoadFromBytes(data []byte) (*Config, error) {
	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode toml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
