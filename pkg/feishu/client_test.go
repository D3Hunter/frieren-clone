package feishu

import (
	"strings"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func TestToLogLevel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		out  larkcore.LogLevel
	}{
		{name: "debug", in: "debug", out: larkcore.LogLevelDebug},
		{name: "info default", in: "", out: larkcore.LogLevelInfo},
		{name: "warn", in: "warn", out: larkcore.LogLevelWarn},
		{name: "error", in: "error", out: larkcore.LogLevelError},
		{name: "unknown", in: "foo", out: larkcore.LogLevelInfo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToLogLevel(tc.in)
			if got != tc.out {
				t.Fatalf("ToLogLevel(%q)=%v want %v", tc.in, got, tc.out)
			}
		})
	}
}

func TestRedactSecretFromMessage(t *testing.T) {
	in := "connected to wss://example.com/ws?token=abc123&app_secret=abc123 secret=abc123"
	out := redactSecretFromMessage(in, "abc123")
	if out == in {
		t.Fatalf("expected redaction to modify message")
	}
	if strings.Contains(out, "abc123") {
		t.Fatalf("expected secret redacted, got %q", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Fatalf("expected redacted marker in %q", out)
	}
}

func TestFormatLarkLogArgs(t *testing.T) {
	got := formatLarkLogArgs([]interface{}{" connected", "to", "ws "})
	if got != "connected to ws" {
		t.Fatalf("unexpected formatted args: %q", got)
	}
}
