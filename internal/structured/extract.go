package structured

import (
	"sort"
	"strings"
)

// Extract finds structured data blocks (markdown tables, HTML tables, repeated
// numbered key:value record series) in source and returns them as clean
// markdown tables, ordered by their position in source. Deterministic, offline.
func Extract(source string) []Block {
	blocks := append([]Block{}, detectMarkdownTables(source)...)
	blocks = append(blocks, detectHTMLTables(source)...)
	blocks = append(blocks, detectRecordSeries(source)...)
	if len(blocks) == 0 {
		return nil
	}

	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].SrcStart != blocks[j].SrcStart {
			return blocks[i].SrcStart < blocks[j].SrcStart
		}
		if blocks[i].Confidence != blocks[j].Confidence {
			return blocks[i].Confidence > blocks[j].Confidence
		}
		return blocks[i].Kind < blocks[j].Kind
	})

	seenMarkdown := make(map[string]struct{}, len(blocks))
	out := make([]Block, 0, len(blocks))
	lastStart := -1
	for _, block := range blocks {
		if block.SrcStart == lastStart {
			continue
		}
		if _, seen := seenMarkdown[block.Markdown]; seen {
			continue
		}
		seenMarkdown[block.Markdown] = struct{}{}
		out = append(out, block)
		lastStart = block.SrcStart
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Render renders blocks as markdown sections.
func Render(blocks []Block) string {
	if len(blocks) == 0 {
		return ""
	}

	sections := make([]string, 0, len(blocks))
	for _, block := range blocks {
		sections = append(sections, "## "+block.Title+"\n\n"+block.Markdown+"\n")
	}
	return strings.Join(sections, "\n")
}
