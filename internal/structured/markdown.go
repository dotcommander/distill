package structured

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	markdownHeaderRow    = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	markdownSeparatorRow = regexp.MustCompile(`^\s*\|[\s:|-]+\|\s*$`)
)

func detectMarkdownTables(source string) []Block {
	lines := splitSourceLines(source)
	inFence := false
	var blocks []Block

	for i := 0; i < len(lines); i++ {
		if isFenceLine(lines[i].text) {
			inFence = !inFence
			continue
		}
		if inFence || !isMarkdownHeaderRow(lines[i].text) || i+1 >= len(lines) {
			continue
		}
		if !isMarkdownSeparatorRow(lines[i+1].text) {
			continue
		}

		end := i + 2
		for end < len(lines) && isMarkdownHeaderRow(lines[end].text) {
			end++
		}

		blocks = append(blocks, Block{
			Kind:       MarkdownTable,
			Title:      fmt.Sprintf("Table %d", len(blocks)+1),
			Markdown:   renderMarkdownTable(lines[i:end]),
			Confidence: 1.0,
			SrcStart:   lines[i].offset,
		})
		i = end - 1
	}

	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

type sourceLine struct {
	text   string
	offset int
}

func splitSourceLines(source string) []sourceLine {
	parts := strings.SplitAfter(source, "\n")
	lines := make([]sourceLine, 0, len(parts))
	offset := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		text := strings.TrimSuffix(part, "\n")
		lines = append(lines, sourceLine{text: text, offset: offset})
		offset += len(part)
	}
	return lines
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "```" || strings.HasPrefix(trimmed, "```")
}

func isMarkdownHeaderRow(line string) bool {
	return markdownHeaderRow.MatchString(line)
}

func isMarkdownSeparatorRow(line string) bool {
	return strings.Contains(line, "-") && markdownSeparatorRow.MatchString(line)
}

func renderMarkdownTable(lines []sourceLine) string {
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, strings.TrimRight(line.text, " \t\r"))
	}
	return strings.Join(rendered, "\n")
}
