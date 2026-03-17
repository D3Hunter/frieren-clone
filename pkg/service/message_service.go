package service

import "strings"

type Processor struct {
	EchoMode     bool
	DefaultReply string
}

func (p Processor) ProcessMessage(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return p.DefaultReply
	}
	if p.EchoMode {
		return trimmed
	}
	return p.DefaultReply
}
