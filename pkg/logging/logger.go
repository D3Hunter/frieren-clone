package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// FormatText renders human-readable logs using zap's console encoder.
	FormatText = "text"
	// FormatJSON renders structured logs using zap's JSON encoder.
	FormatJSON = "json"
)

// Options controls logger level and encoding format.
type Options struct {
	Level      string
	Format     string
	OutputPath string
}

// New creates a configured zap logger using normalized format and validated level.
func New(options Options) (*zap.Logger, error) {
	level, err := parseLevel(options.Level)
	if err != nil {
		return nil, err
	}

	format := normalizeFormat(options.Format)
	encoding := "console"
	if format == FormatJSON {
		encoding = "json"
	}
	outputPath, errorOutputPath, err := normalizeOutputPath(options.OutputPath)
	if err != nil {
		return nil, err
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      false,
		Encoding:         encoding,
		OutputPaths:      []string{outputPath},
		ErrorOutputPaths: []string{errorOutputPath},
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
	}

	return cfg.Build()
}

func normalizeOutputPath(outputPath string) (string, string, error) {
	trimmedOutputPath := strings.TrimSpace(outputPath)
	if trimmedOutputPath == "" {
		trimmedOutputPath = filepath.Join("logs", "frieren.log")
	}
	switch trimmedOutputPath {
	case "stdout":
		return "stdout", "stderr", nil
	case "stderr":
		return "stderr", "stderr", nil
	}
	outputDir := filepath.Dir(trimmedOutputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return "", "", fmt.Errorf("create log directory %q: %w", outputDir, err)
		}
	}
	return trimmedOutputPath, trimmedOutputPath, nil
}

func normalizeFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatJSON:
		return FormatJSON
	default:
		return FormatText
	}
}

func parseLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("invalid log level %q", level)
	}
}
