package structured

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractReturnsDetectedBlocksInSourceOrder(t *testing.T) {
	t.Parallel()

	source := strings.Join([]string{
		"intro",
		"",
		"| Name | Value |",
		"| --- | --- |",
		"| alpha | 1 |",
		"| beta | 2 |",
		"",
		"between",
		"",
		"Sensor #1",
		"Temp: 70F",
		"Humidity: 40%",
		"Sensor #2",
		"Temp: 71F",
		"Humidity: 41%",
		"Sensor #3",
		"Temp: 72F",
		"Humidity: 42%",
	}, "\n")

	blocks := Extract(source)

	require.Len(t, blocks, 2)
	assert.Equal(t, MarkdownTable, blocks[0].Kind)
	assert.Equal(t, RecordSeries, blocks[1].Kind)
	assert.Less(t, blocks[0].SrcStart, blocks[1].SrcStart)
}

func TestExtractReturnsNilForPlainProse(t *testing.T) {
	t.Parallel()

	blocks := Extract("plain prose without structured data\njust another sentence")

	require.Nil(t, blocks)
}

func TestExtractDeduplicatesSingleMarkdownTable(t *testing.T) {
	t.Parallel()

	source := strings.Join([]string{
		"| Name | Value |",
		"| --- | --- |",
		"| alpha | 1 |",
	}, "\n")

	blocks := Extract(source)

	require.Len(t, blocks, 1)
	assert.Equal(t, MarkdownTable, blocks[0].Kind)
}

func TestRender(t *testing.T) {
	t.Parallel()

	blocks := []Block{
		{
			Kind:     MarkdownTable,
			Title:    "Table 1",
			Markdown: "| A | B |\n| --- | --- |\n| 1 | 2 |",
		},
		{
			Kind:     RecordSeries,
			Title:    "Sensor",
			Markdown: "| # | Temp |\n| --- | --- |\n| 1 | 70F |",
		},
	}

	rendered := Render(blocks)

	assert.Contains(t, rendered, "## Table 1")
	assert.Contains(t, rendered, "## Sensor")
	assert.Contains(t, rendered, blocks[0].Markdown)
	assert.Contains(t, rendered, blocks[1].Markdown)
	assert.Contains(t, rendered, blocks[0].Markdown+"\n\n## Sensor")
	assert.Equal(t, "", Render(nil))
}
