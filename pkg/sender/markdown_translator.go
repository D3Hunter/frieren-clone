package sender

import (
	"strings"

	"github.com/D3Hunter/frieren-clone/pkg/feishumarkdown"
)

func translateCodexMarkdownToFeishu(input string) (string, error) {
	return feishumarkdown.TranslateCodexMarkdownToFeishu(input)
}

// Keep the legacy helper in sender until the remaining sender-local translator
// tests are fully rebalanced into pkg/feishumarkdown in a later milestone.
func renderInlineCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "``"
	}
	value = strings.ReplaceAll(value, "\n", " ")
	fenceLen := maxConsecutiveRunes(value, '`') + 1
	if fenceLen < 1 {
		fenceLen = 1
	}
	fence := strings.Repeat("`", fenceLen)
	return fence + value + fence
}

func maxConsecutiveRunes(value string, target rune) int {
	maxRun := 0
	current := 0
	for _, r := range value {
		if r == target {
			current++
			if current > maxRun {
				maxRun = current
			}
			continue
		}
		current = 0
	}
	return maxRun
}
