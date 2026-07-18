package structured

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectHTMLTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected []Block
	}{
		{
			name:   "thead table renders markdown",
			source: "before <table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table> after",
			expected: []Block{
				{
					Kind:       HTMLTable,
					Title:      "Table 1",
					Markdown:   "| A | B |\n| --- | --- |\n| 1 | 2 |",
					Confidence: 1.0,
					SrcStart:   len("before "),
				},
			},
		},
		{
			name:   "plain prose fast path",
			source: "plain prose without any markup",
		},
		{
			name:   "first td row becomes header",
			source: "<table><tr><td>A</td><td>B</td></tr><tr><td>1</td><td>2</td></tr></table>",
			expected: []Block{
				{
					Kind:       HTMLTable,
					Title:      "Table 1",
					Markdown:   "| A | B |\n| --- | --- |\n| 1 | 2 |",
					Confidence: 1.0,
					SrcStart:   0,
				},
			},
		},
		{
			name:   "pipes are escaped in cells",
			source: "<table><tr><th>A</th></tr><tr><td>x | y</td></tr></table>",
			expected: []Block{
				{
					Kind:       HTMLTable,
					Title:      "Table 1",
					Markdown:   "| A |\n| --- |\n| x \\| y |",
					Confidence: 1.0,
					SrcStart:   0,
				},
			},
		},
		{
			name:   "two tables get increasing titles",
			source: "<table><tr><th>A</th></tr><tr><td>1</td></tr></table> prose <table><tr><th>B</th></tr><tr><td>2</td></tr></table>",
			expected: []Block{
				{
					Kind:       HTMLTable,
					Title:      "Table 1",
					Markdown:   "| A |\n| --- |\n| 1 |",
					Confidence: 1.0,
					SrcStart:   0,
				},
				{
					Kind:       HTMLTable,
					Title:      "Table 2",
					Markdown:   "| B |\n| --- |\n| 2 |",
					Confidence: 1.0,
					SrcStart:   len("<table><tr><th>A</th></tr><tr><td>1</td></tr></table> prose "),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocks := detectHTMLTables(tt.source)
			if len(tt.expected) == 0 {
				require.Nil(t, blocks)
				return
			}

			require.Len(t, blocks, len(tt.expected))
			for i, expected := range tt.expected {
				assert.Equal(t, expected.Kind, blocks[i].Kind)
				assert.Equal(t, expected.Title, blocks[i].Title)
				assert.Equal(t, expected.Markdown, blocks[i].Markdown)
				assert.Equal(t, expected.Confidence, blocks[i].Confidence)
				assert.Equal(t, expected.SrcStart, blocks[i].SrcStart)
			}
			for i := 1; i < len(blocks); i++ {
				assert.Less(t, blocks[i-1].SrcStart, blocks[i].SrcStart)
			}
		})
	}
}
