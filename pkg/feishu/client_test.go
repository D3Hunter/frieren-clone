package feishu

import (
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
