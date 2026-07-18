package structured

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectMarkdownTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected []Block
	}{
		{
			name:   "clean two column table between prose",
			source: "before\n\n| Name | Value |\n| --- | --- |\n| alpha | 1 |\n| beta | 2 |\n\nafter\n",
			expected: []Block{
				{
					Kind:       MarkdownTable,
					Title:      "Table 1",
					Markdown:   "| Name | Value |\n| --- | --- |\n| alpha | 1 |\n| beta | 2 |",
					Confidence: 1.0,
					SrcStart:   len("before\n\n"),
				},
			},
		},
		{
			name:   "fenced table ignored",
			source: "before\n```markdown\n| Name | Value |\n| --- | --- |\n| alpha | 1 |\n```\nafter\n",
		},
		{
			name:   "minimum table without body rows",
			source: "| Name | Value |\n| --- | --- |\n",
			expected: []Block{
				{
					Kind:       MarkdownTable,
					Title:      "Table 1",
					Markdown:   "| Name | Value |\n| --- | --- |",
					Confidence: 1.0,
					SrcStart:   0,
				},
			},
		},
		{
			name:   "two separate tables",
			source: "| A | B |\n| - | - |\n| 1 | 2 |\n\nprose\n\n| C | D |\n| - | - |\n| 3 | 4 |\n",
			expected: []Block{
				{
					Kind:       MarkdownTable,
					Title:      "Table 1",
					Markdown:   "| A | B |\n| - | - |\n| 1 | 2 |",
					Confidence: 1.0,
					SrcStart:   0,
				},
				{
					Kind:       MarkdownTable,
					Title:      "Table 2",
					Markdown:   "| C | D |\n| - | - |\n| 3 | 4 |",
					Confidence: 1.0,
					SrcStart:   len("| A | B |\n| - | - |\n| 1 | 2 |\n\nprose\n\n"),
				},
			},
		},
		{
			name:   "stray pipes without separator ignored",
			source: "This prose has | pipes | in it.\n| also | looks | rowish |\nnot a separator\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocks := detectMarkdownTables(tt.source)
			if len(tt.expected) == 0 {
				require.Empty(t, blocks)
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
