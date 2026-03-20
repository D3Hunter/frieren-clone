package feishumarkdown

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var markdownListItemPattern = regexp.MustCompile(`^\s{0,3}(?:[-*+]|\d+[.)])\s+`)
var markdownTableSeparatorPattern = regexp.MustCompile(`^\s*\|?(?:\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$`)

// SplitTranslatedMarkdown splits already-translated Feishu markdown into safe
// interactive-card chunks while preserving markdown structures where possible.
func SplitTranslatedMarkdown(input string, maxRunes int) []string {
	return splitMarkdownChunks(input, maxRunes)
}

func splitMarkdownChunks(input string, maxRunes int) []string {
	input = normalizeMarkdown(input)
	if maxRunes <= 0 || utf8.RuneCountInString(input) <= maxRunes {
		return []string{input}
	}

	blocks := splitMarkdownBlocks(input)
	if len(blocks) == 0 {
		return splitPlainTextChunks(input, maxRunes)
	}

	chunks := make([]string, 0, len(blocks))
	currentBlocks := make([]string, 0, 8)
	currentRunes := 0
	flushCurrent := func() {
		if len(currentBlocks) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(currentBlocks, ""))
		currentBlocks = currentBlocks[:0]
		currentRunes = 0
	}
	appendBlock := func(block string) {
		currentBlocks = append(currentBlocks, block)
		currentRunes += utf8.RuneCountInString(block)
	}

	for _, block := range blocks {
		if block == "" {
			continue
		}
		blockRunes := utf8.RuneCountInString(block)

		if len(currentBlocks) == 0 {
			if blockRunes <= maxRunes {
				appendBlock(block)
				continue
			}
			forced := splitOversizedMarkdownBlock(block, maxRunes)
			chunks = append(chunks, forced...)
			continue
		}

		if currentRunes+blockRunes <= maxRunes {
			appendBlock(block)
			continue
		}

		if headingTail, ok := headingCarryTailBlocks(currentBlocks, block); ok {
			// Keep heading lines attached to their following block. Without this carry-over, a table/list/code
			// block can start a new chunk with no section title, and some Feishu markdown cards render poorly.
			tailRunes := runeCountOfBlocks(headingTail)
			currentBlocks = currentBlocks[:len(currentBlocks)-len(headingTail)]
			currentRunes -= tailRunes
			flushCurrent()

			currentBlocks = append(currentBlocks, headingTail...)
			currentRunes = tailRunes
			if currentRunes+blockRunes <= maxRunes {
				appendBlock(block)
				continue
			}

			flushCurrent()
			if blockRunes <= maxRunes {
				appendBlock(block)
				continue
			}
			forced := splitOversizedMarkdownBlock(block, maxRunes)
			chunks = append(chunks, forced...)
			continue
		}

		flushCurrent()
		if blockRunes <= maxRunes {
			appendBlock(block)
			continue
		}
		forced := splitOversizedMarkdownBlock(block, maxRunes)
		chunks = append(chunks, forced...)
	}

	flushCurrent()
	if len(chunks) == 0 {
		return []string{input}
	}
	return chunks
}

func splitMarkdownBlocks(input string) []string {
	lines := strings.SplitAfter(input, "\n")
	if len(lines) == 0 {
		return []string{input}
	}
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	blocks := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		line := trimLineEnding(lines[i])
		trimmed := strings.TrimSpace(line)
		start := i

		if trimmed == "" {
			for i < len(lines) && strings.TrimSpace(trimLineEnding(lines[i])) == "" {
				i++
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		if fenceChar, fenceLen, ok := parseFenceMarker(trimmed); ok {
			i++
			for i < len(lines) {
				next := strings.TrimSpace(trimLineEnding(lines[i]))
				i++
				if isStrictFenceCloser(next, fenceChar, fenceLen) {
					break
				}
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		if i+1 < len(lines) && isTableHeaderLine(trimLineEnding(lines[i])) && isTableSeparatorLine(trimLineEnding(lines[i+1])) {
			i += 2
			for i < len(lines) && isTableRowLine(trimLineEnding(lines[i])) {
				i++
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		if isListItemLine(line) {
			i++
			for i < len(lines) {
				current := trimLineEnding(lines[i])
				trimmedCurrent := strings.TrimSpace(current)
				if trimmedCurrent == "" {
					break
				}
				if fenceChar, fenceLen, ok := parseFenceMarker(strings.TrimSpace(current)); ok {
					i++
					for i < len(lines) {
						next := strings.TrimSpace(trimLineEnding(lines[i]))
						i++
						if isStrictFenceCloser(next, fenceChar, fenceLen) {
							break
						}
					}
					continue
				}
				if isListItemLine(current) || isListContinuationLine(current) {
					i++
					continue
				}
				break
			}
			blocks = append(blocks, strings.Join(lines[start:i], ""))
			continue
		}

		i++
		for i < len(lines) {
			current := trimLineEnding(lines[i])
			if strings.TrimSpace(current) == "" {
				break
			}
			if _, _, ok := parseFenceMarker(strings.TrimSpace(current)); ok {
				break
			}
			if i+1 < len(lines) && isTableHeaderLine(current) && isTableSeparatorLine(trimLineEnding(lines[i+1])) {
				break
			}
			if isListItemLine(current) {
				break
			}
			i++
		}
		blocks = append(blocks, strings.Join(lines[start:i], ""))
	}
	return blocks
}

func splitOversizedMarkdownBlock(block string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{block}
	}
	if utf8.RuneCountInString(block) <= maxRunes {
		return []string{block}
	}

	lines := strings.SplitAfter(block, "\n")
	if len(lines) == 0 {
		return splitPlainTextChunks(block, maxRunes)
	}
	trimmedStart := strings.TrimSpace(trimLineEnding(lines[0]))
	fenceChar, fenceLen, ok := parseFenceMarker(trimmedStart)
	if !ok {
		return splitPlainTextChunks(block, maxRunes)
	}

	closingIndex := -1
	for i := len(lines) - 1; i >= 1; i-- {
		candidate := strings.TrimSpace(trimLineEnding(lines[i]))
		if isStrictFenceCloser(candidate, fenceChar, fenceLen) {
			closingIndex = i
			break
		}
	}
	if closingIndex <= 0 {
		return splitPlainTextChunks(block, maxRunes)
	}

	openFence := trimLineEnding(lines[0])
	closeFence := trimLineEnding(lines[closingIndex])
	// This guard handles pathological tiny chunk caps where one fenced chunk cannot fit even with empty body.
	minWrapperRunes := utf8.RuneCountInString(openFence) + utf8.RuneCountInString(closeFence) + 2
	if minWrapperRunes > maxRunes {
		return splitPlainTextChunks(block, maxRunes)
	}

	body := strings.Join(lines[1:closingIndex], "")
	body = strings.TrimSuffix(body, "\n")
	bodyMaxRunes := maxRunes - minWrapperRunes
	if bodyMaxRunes <= 0 && strings.TrimSpace(body) != "" {
		// When the fence wrapper alone consumes the full budget, preserving balanced fences is impossible
		// without exceeding the cap, so fall back to plain splitting to keep the hard size limit intact.
		return splitPlainTextChunks(block, maxRunes)
	}
	bodyParts := []string{""}
	if strings.TrimSpace(body) != "" {
		bodyParts = splitPlainTextChunks(body, bodyMaxRunes)
	}

	chunks := make([]string, 0, len(bodyParts)+1)
	for _, part := range bodyParts {
		part = strings.TrimSuffix(part, "\n")
		chunks = append(chunks, openFence+"\n"+part+"\n"+closeFence)
	}

	suffix := strings.Join(lines[closingIndex+1:], "")
	if strings.TrimSpace(suffix) == "" {
		return chunks
	}
	return append(chunks, splitMarkdownChunks(suffix, maxRunes)...)
}

func splitPlainTextChunks(input string, maxRunes int) []string {
	if maxRunes <= 0 || utf8.RuneCountInString(input) <= maxRunes {
		return []string{input}
	}

	remaining := []rune(input)
	chunks := make([]string, 0, len(remaining)/maxRunes+1)
	for len(remaining) > 0 {
		if len(remaining) <= maxRunes {
			chunks = append(chunks, string(remaining))
			break
		}

		cut := chooseChunkCut(remaining, maxRunes)
		chunks = append(chunks, string(remaining[:cut]))
		remaining = remaining[cut:]
	}
	return chunks
}

func chooseChunkCut(runes []rune, maxRunes int) int {
	if len(runes) <= maxRunes {
		return len(runes)
	}

	// Prefer keeping whole lines to preserve list and paragraph readability.
	for i := maxRunes - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}

	// If a single line is too long, keep whole words when possible.
	for i := maxRunes - 1; i >= 0; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1
		}
	}

	// Last resort for overlong single tokens (for example very long URLs).
	return maxRunes
}

func headingCarryTailBlocks(currentBlocks []string, nextBlock string) ([]string, bool) {
	if len(currentBlocks) == 0 {
		return nil, false
	}
	if strings.TrimSpace(nextBlock) == "" {
		return nil, false
	}
	lastContent := len(currentBlocks) - 1
	for lastContent >= 0 && strings.TrimSpace(currentBlocks[lastContent]) == "" {
		lastContent--
	}
	if lastContent < 0 || !isHeadingBlock(currentBlocks[lastContent]) {
		return nil, false
	}
	return append([]string(nil), currentBlocks[lastContent:]...), true
}

func isHeadingBlock(block string) bool {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return false
	}

	hashCount := 0
	for hashCount < len(trimmed) && trimmed[hashCount] == '#' {
		hashCount++
	}
	if hashCount == 0 || hashCount > 6 {
		return false
	}
	if len(trimmed) == hashCount {
		return false
	}
	return trimmed[hashCount] == ' '
}

func runeCountOfBlocks(blocks []string) int {
	total := 0
	for _, block := range blocks {
		total += utf8.RuneCountInString(block)
	}
	return total
}

func trimLineEnding(line string) string {
	return strings.TrimRight(line, "\n")
}

func isListItemLine(line string) bool {
	return markdownListItemPattern.MatchString(line)
}

func isListContinuationLine(line string) bool {
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

func isTableHeaderLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if !strings.Contains(trimmed, "|") {
		return false
	}
	return !markdownTableSeparatorPattern.MatchString(trimmed)
}

func isTableSeparatorLine(line string) bool {
	return markdownTableSeparatorPattern.MatchString(strings.TrimSpace(line))
}

func isTableRowLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if markdownTableSeparatorPattern.MatchString(trimmed) {
		return false
	}
	return strings.Contains(trimmed, "|")
}

func withChunkSuffix(chunk string, index, total int) string {
	return fmt.Sprintf("%s\n\n[%d/%d]", strings.TrimRight(chunk, "\n"), index+1, total)
}
