package structured

// Kind classifies a detected structured block.
type Kind int

const (
	MarkdownTable Kind = iota
	HTMLTable
	RecordSeries
)

func (k Kind) String() string {
	switch k {
	case MarkdownTable:
		return "markdown-table"
	case HTMLTable:
		return "html-table"
	case RecordSeries:
		return "record-series"
	default:
		return "unknown"
	}
}

// Block is a structured region detected in a source document, rendered as a
// clean markdown table.
type Block struct {
	Kind       Kind
	Title      string  // short label, e.g. "Table 1"
	Markdown   string  // clean rendered markdown table (no surrounding blank lines)
	Confidence float64 // 0..1; tables are 1.0
	SrcStart   int     // byte offset of the block start in source, for ordering
}
