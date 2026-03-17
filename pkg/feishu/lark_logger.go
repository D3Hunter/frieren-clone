package feishu

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"go.uber.org/zap"
)

type larkZapLogger struct {
	logger    *zap.Logger
	appSecret string
}

var querySecretPattern = regexp.MustCompile(`(?i)([?&][^=\s&]*?(?:token|secret|ticket|sign|signature|password|key)[^=\s&]*=)([^&\s]+)`)
var jsonSecretPattern = regexp.MustCompile(`(?i)("(?:app_secret|secret|token|ticket|password)"\s*:\s*")([^"]+)(")`)
var assignmentSecretPattern = regexp.MustCompile(`(?i)\b(app_secret|secret|token|ticket|password)\s*=\s*([^\s,;]+)`)

func newLarkZapLogger(logger *zap.Logger, appSecret string) larkcore.Logger {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &larkZapLogger{
		logger:    logger,
		appSecret: strings.TrimSpace(appSecret),
	}
}

// Debug emits a redacted debug-level SDK log message through zap.
func (l *larkZapLogger) Debug(ctx context.Context, args ...interface{}) {
	l.log(ctx, l.logger.Debug, args...)
}

// Info emits a redacted info-level SDK log message through zap.
func (l *larkZapLogger) Info(ctx context.Context, args ...interface{}) {
	l.log(ctx, l.logger.Info, args...)
}

// Warn emits a redacted warn-level SDK log message through zap.
func (l *larkZapLogger) Warn(ctx context.Context, args ...interface{}) {
	l.log(ctx, l.logger.Warn, args...)
}

// Error emits a redacted error-level SDK log message through zap.
func (l *larkZapLogger) Error(ctx context.Context, args ...interface{}) {
	l.log(ctx, l.logger.Error, args...)
}

func (l *larkZapLogger) log(ctx context.Context, emit func(string, ...zap.Field), args ...interface{}) {
	_ = ctx
	emit(redactSecretFromMessage(formatLarkLogArgs(args), l.appSecret))
}

func formatLarkLogArgs(args []interface{}) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, strings.TrimSpace(fmt.Sprint(arg)))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func redactSecretFromMessage(message, appSecret string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}

	if strings.TrimSpace(appSecret) != "" {
		message = strings.ReplaceAll(message, appSecret, "***")
	}
	message = querySecretPattern.ReplaceAllString(message, "${1}REDACTED")
	message = jsonSecretPattern.ReplaceAllString(message, "${1}REDACTED${3}")
	message = assignmentSecretPattern.ReplaceAllString(message, "${1}=REDACTED")
	return message
}
