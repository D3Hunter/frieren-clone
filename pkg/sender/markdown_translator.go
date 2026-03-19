package sender

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

func translateCodexMarkdownToFeishu(input string) (string, error) {
	normalized := unwrapTopLevelMarkdownFence(normalizeMarkdown(input))
	if strings.TrimSpace(normalized) == "" {
		return normalized, nil
	}

	source := []byte(normalized)
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM))
	document := markdown.Parser().Parse(text.NewReader(source))
	renderer := markdownASTRenderer{source: source}
	output := renderer.renderDocument(document)
	if strings.TrimSpace(output) == "" {
		return normalizeMarkdown(input), nil
	}
	return output, nil
}

func unwrapTopLevelMarkdownFence(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return input
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return input
	}

	firstLine := strings.TrimSpace(lines[0])
	fenceChar, fenceLen, ok := parseFenceMarker(firstLine)
	if !ok {
		return input
	}
	fenceInfo := strings.TrimSpace(firstLine[fenceLen:])
	if !isMarkdownFenceInfo(fenceInfo) {
		return input
	}

	closingIndex := -1
	for i := len(lines) - 1; i >= 1; i-- {
		if isStrictFenceCloser(strings.TrimSpace(lines[i]), fenceChar, fenceLen) {
			closingIndex = i
			break
		}
	}
	if closingIndex <= 0 {
		return input
	}

	body := strings.TrimSpace(strings.Join(lines[1:closingIndex], "\n"))
	suffix := strings.TrimSpace(strings.Join(lines[closingIndex+1:], "\n"))
	switch {
	case body == "" && suffix == "":
		return ""
	case body == "":
		return suffix
	case suffix == "":
		return body
	default:
		return body + "\n\n" + suffix
	}
}

func isMarkdownFenceInfo(info string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(info)))
	if len(fields) == 0 {
		return false
	}
	lang := strings.Trim(fields[0], "{}")
	switch lang {
	case "markdown", "md", "mdown", "mkdn", "mkd", "commonmark":
		return true
	default:
		return false
	}
}

func isStrictFenceCloser(line string, fenceChar rune, fenceLen int) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	count := 0
	for _, r := range line {
		if r == fenceChar {
			count++
			continue
		}
		break
	}
	if count < fenceLen {
		return false
	}
	return strings.TrimSpace(line[count:]) == ""
}

type markdownASTRenderer struct {
	source []byte
}

func (r markdownASTRenderer) renderDocument(document gast.Node) string {
	blocks := make([]string, 0, document.ChildCount())
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		block := strings.TrimRight(r.renderBlock(child, 0), "\n")
		if strings.TrimSpace(block) == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

func (r markdownASTRenderer) renderBlock(node gast.Node, listDepth int) string {
	switch typed := node.(type) {
	case *gast.Heading:
		level := typed.Level
		// Observed in Feishu card rendering: top-level h1 may disappear in some payloads.
		// Downgrade h1 to h2 as a compatibility workaround so the title stays visible.
		if level < 2 {
			level = 2
		}
		if level > 6 {
			level = 6
		}
		return strings.Repeat("#", level) + " " + strings.TrimSpace(r.renderInlineChildren(typed))
	case *gast.Paragraph:
		return strings.TrimSpace(r.renderInlineChildren(typed))
	case *gast.TextBlock:
		return strings.TrimSpace(r.renderInlineChildren(typed))
	case *gast.FencedCodeBlock:
		language := strings.TrimSpace(string(typed.Language(r.source)))
		content := strings.TrimRight(blockLinesValue(typed.Lines(), r.source), "\n")
		if language != "" {
			return fmt.Sprintf("```%s\n%s\n```", language, content)
		}
		return fmt.Sprintf("```\n%s\n```", content)
	case *gast.CodeBlock:
		content := strings.TrimRight(blockLinesValue(typed.Lines(), r.source), "\n")
		return fmt.Sprintf("```\n%s\n```", content)
	case *gast.Blockquote:
		inner := make([]string, 0, typed.ChildCount())
		for child := typed.FirstChild(); child != nil; child = child.NextSibling() {
			rendered := strings.TrimSpace(r.renderBlock(child, listDepth))
			if rendered == "" {
				continue
			}
			inner = append(inner, rendered)
		}
		return prefixEachLine(strings.Join(inner, "\n\n"), "> ")
	case *gast.List:
		return r.renderList(typed, listDepth)
	case *extast.Table:
		return r.renderTable(typed)
	case *gast.ThematicBreak:
		return "---"
	case *gast.HTMLBlock:
		raw := strings.TrimRight(blockLinesValue(typed.Lines(), r.source), "\n")
		if typed.HasClosure() {
			raw += string(typed.ClosureLine.Value(r.source))
		}
		return html.EscapeString(raw)
	default:
		if typed != nil && typed.HasChildren() {
			parts := make([]string, 0, typed.ChildCount())
			for child := typed.FirstChild(); child != nil; child = child.NextSibling() {
				rendered := strings.TrimSpace(r.renderBlock(child, listDepth))
				if rendered == "" {
					continue
				}
				parts = append(parts, rendered)
			}
			return strings.Join(parts, "\n\n")
		}
		return strings.TrimSpace(string(node.Text(r.source)))
	}
}

func (r markdownASTRenderer) renderList(list *gast.List, listDepth int) string {
	lines := make([]string, 0, list.ChildCount())
	nextNumber := list.Start
	if nextNumber <= 0 {
		nextNumber = 1
	}

	index := 0
	for itemNode := list.FirstChild(); itemNode != nil; itemNode = itemNode.NextSibling() {
		item, ok := itemNode.(*gast.ListItem)
		if !ok {
			continue
		}
		marker := "-"
		if list.IsOrdered() {
			marker = fmt.Sprintf("%d.", nextNumber+index)
		}
		index++

		itemLines := r.renderListItem(item, listDepth+1)
		indent := strings.Repeat("  ", listDepth)
		if len(itemLines) == 0 {
			lines = append(lines, indent+marker)
			continue
		}

		lines = append(lines, indent+marker+" "+strings.TrimSpace(itemLines[0]))
		for _, line := range itemLines[1:] {
			if strings.TrimSpace(line) == "" {
				lines = append(lines, "")
				continue
			}
			if strings.HasPrefix(line, strings.Repeat("  ", listDepth+1)) {
				lines = append(lines, line)
				continue
			}
			lines = append(lines, indent+"  "+line)
		}
	}
	return strings.Join(lines, "\n")
}

func (r markdownASTRenderer) renderListItem(item *gast.ListItem, listDepth int) []string {
	lines := make([]string, 0, item.ChildCount())
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *gast.List:
			nested := r.renderList(typed, listDepth)
			if strings.TrimSpace(nested) != "" {
				lines = append(lines, strings.Split(nested, "\n")...)
			}
		case *gast.Paragraph, *gast.TextBlock:
			text := strings.TrimSpace(r.renderInlineChildren(typed))
			if text != "" {
				lines = append(lines, text)
			}
		default:
			block := strings.TrimSpace(r.renderBlock(child, listDepth))
			if block == "" {
				continue
			}
			lines = append(lines, strings.Split(block, "\n")...)
		}
	}
	return lines
}

func (r markdownASTRenderer) renderTable(table *extast.Table) string {
	header, rows := extractTableRows(table, r.source)
	if len(header) == 0 && len(rows) == 0 {
		return ""
	}

	columnCount := len(header)
	if columnCount < len(table.Alignments) {
		columnCount = len(table.Alignments)
	}
	for _, row := range rows {
		if len(row) > columnCount {
			columnCount = len(row)
		}
	}
	if columnCount == 0 {
		columnCount = 1
	}

	header = padCells(header, columnCount)
	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, formatTableRow(header))
	lines = append(lines, formatTableAlignmentRow(table.Alignments, columnCount))
	for _, row := range rows {
		lines = append(lines, formatTableRow(padCells(row, columnCount)))
	}
	return strings.Join(lines, "\n")
}

func extractTableRows(table *extast.Table, source []byte) ([]string, [][]string) {
	header := []string{}
	rows := [][]string{}
	for rowNode := table.FirstChild(); rowNode != nil; rowNode = rowNode.NextSibling() {
		switch typed := rowNode.(type) {
		case *extast.TableHeader:
			header = renderTableCells(typed, source)
		case *extast.TableRow:
			rows = append(rows, renderTableCells(typed, source))
		}
	}
	return header, rows
}

func renderTableCells(row gast.Node, source []byte) []string {
	cells := make([]string, 0, row.ChildCount())
	for cellNode := row.FirstChild(); cellNode != nil; cellNode = cellNode.NextSibling() {
		cellText := strings.TrimSpace(renderNodeInlineText(cellNode, source))
		cellText = strings.ReplaceAll(cellText, "\n", " ")
		cellText = strings.ReplaceAll(cellText, "|", `\|`)
		cellText = sanitizeTableCellBackticks(cellText)
		cells = append(cells, cellText)
	}
	return cells
}

func sanitizeTableCellBackticks(cellText string) string {
	if strings.Count(cellText, "`")%2 == 0 {
		return cellText
	}
	// Some model outputs place unescaped pipes inside table cell code spans (for example `| a | b |`),
	// which can be parsed into dangling single backticks. Escape those backticks to keep Feishu markdown renderable.
	return strings.ReplaceAll(cellText, "`", "\\`")
}

func padCells(cells []string, size int) []string {
	if len(cells) >= size {
		return cells
	}
	out := make([]string, size)
	copy(out, cells)
	for i := len(cells); i < size; i++ {
		out[i] = ""
	}
	return out
}

func formatTableRow(cells []string) string {
	return "| " + strings.Join(cells, " | ") + " |"
}

func formatTableAlignmentRow(alignments []extast.Alignment, columnCount int) string {
	columns := make([]string, columnCount)
	for i := 0; i < columnCount; i++ {
		align := extast.AlignNone
		if i < len(alignments) {
			align = alignments[i]
		}
		switch align {
		case extast.AlignLeft:
			columns[i] = ":---"
		case extast.AlignRight:
			columns[i] = "---:"
		case extast.AlignCenter:
			columns[i] = ":---:"
		default:
			columns[i] = "---"
		}
	}
	return "| " + strings.Join(columns, " | ") + " |"
}

func (r markdownASTRenderer) renderInlineChildren(parent gast.Node) string {
	return renderNodeInlineText(parent, r.source)
}

func renderNodeInlineText(parent gast.Node, source []byte) string {
	builder := strings.Builder{}
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *gast.Text:
			builder.Write(typed.Value(source))
			if typed.HardLineBreak() {
				builder.WriteString("  \n")
			} else if typed.SoftLineBreak() {
				builder.WriteByte('\n')
			}
		case *gast.String:
			builder.Write(typed.Value)
		case *gast.Emphasis:
			marker := "*"
			if typed.Level >= 2 {
				marker = "**"
			}
			builder.WriteString(marker)
			builder.WriteString(renderNodeInlineText(typed, source))
			builder.WriteString(marker)
		case *extast.Strikethrough:
			builder.WriteString("~~")
			builder.WriteString(renderNodeInlineText(typed, source))
			builder.WriteString("~~")
		case *gast.CodeSpan:
			code := strings.TrimSpace(renderNodeInlineText(typed, source))
			builder.WriteString(renderInlineCode(code))
		case *extast.TaskCheckBox:
			if typed.IsChecked {
				builder.WriteString("[x] ")
			} else {
				builder.WriteString("[ ] ")
			}
		case *gast.Link:
			destination := strings.TrimSpace(string(typed.Destination))
			label := strings.TrimSpace(renderNodeInlineText(typed, source))
			if isHTTPLink(destination) {
				if label == "" {
					label = destination
				}
				builder.WriteString("[" + label + "](" + destination + ")")
				continue
			}
			if label == "" || strings.EqualFold(label, destination) {
				builder.WriteString(renderInlineCode(destination))
			} else {
				builder.WriteString(label + " (" + renderInlineCode(destination) + ")")
			}
		case *gast.AutoLink:
			destination := strings.TrimSpace(string(typed.URL(source)))
			if isHTTPLink(destination) {
				builder.WriteString("<" + destination + ">")
			} else {
				builder.WriteString(renderInlineCode(destination))
			}
		case *gast.Image:
			destination := strings.TrimSpace(string(typed.Destination))
			alt := strings.TrimSpace(renderNodeInlineText(typed, source))
			if alt == "" {
				alt = "image"
			}
			if isHTTPLink(destination) {
				builder.WriteString("![" + alt + "](" + destination + ")")
				continue
			}
			builder.WriteString(alt + " (" + renderInlineCode(destination) + ")")
		case *gast.RawHTML:
			raw := strings.TrimSpace(string(typed.Segments.Value(source)))
			builder.WriteString(html.EscapeString(raw))
		default:
			if typed != nil && typed.HasChildren() {
				builder.WriteString(renderNodeInlineText(typed, source))
			} else {
				builder.WriteString(string(child.Text(source)))
			}
		}
	}
	return builder.String()
}

func blockLinesValue(lines *text.Segments, source []byte) string {
	if lines == nil || lines.Len() == 0 {
		return ""
	}
	builder := strings.Builder{}
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		builder.Write(segment.Value(source))
	}
	return builder.String()
}

func prefixEachLine(value, prefix string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = strings.TrimSpace(prefix)
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func isHTTPLink(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	return scheme == "http" || scheme == "https"
}

func renderInlineCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "``"
	}
	if strings.Contains(value, "`") {
		return "``" + strings.ReplaceAll(value, "\n", " ") + "``"
	}
	return "`" + strings.ReplaceAll(value, "\n", " ") + "`"
}
