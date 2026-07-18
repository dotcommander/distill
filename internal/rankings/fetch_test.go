package rankings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMarkdownTableRows(t *testing.T) {
	t.Parallel()

	md := `intro
| Rank | Model | Score |
| ---: | :---- | ----: |
| 1 | GLM-5.2 | 88.0% |
| 2 | GPT-5 | 84 |

| | Model | Edit |
|---|---|---|
| | Qwen | 29.08 |
`

	rows := ParseMarkdownTableRows(md)

	require.Equal(t, [][]string{
		{"Rank", "Model", "Score"},
		{"1", "GLM-5.2", "88.0%"},
		{"2", "GPT-5", "84"},
		{"", "Model", "Edit"},
		{"", "Qwen", "29.08"},
	}, rows)
}

func TestExtractScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cell string
		want float64
		ok   bool
	}{
		{name: "percent", cell: "88.0%", want: 88, ok: true},
		{name: "currency", cell: "$29.08", want: 29.08, ok: true},
		{name: "decimal", cell: "3.3", want: 3.3, ok: true},
		{name: "comma", cell: "1,234", want: 1234, ok: true},
		{name: "missing", cell: "diff", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ExtractScore(tt.cell)

			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestMatchScoresHigherIsBetter(t *testing.T) {
	t.Parallel()

	board := Board{
		ModelCol: 0,
		ScoreCol: 1,
		Aliases: map[string]string{
			"glm-5.2": "z-ai/glm-5.2",
		},
	}
	roster := []string{"z-ai/glm-5.2", "deepseek/deepseek-v4-pro"}
	rows := [][]string{
		{"GLM-5.2", "88.0%"},
		{"GPT-5", "90"},
		{"glm-5.2 (high)", "91"},
	}

	scores, matched, unmatched := MatchScores(board, rows, roster)

	assert.Equal(t, map[string]float64{"z-ai/glm-5.2": 91}, scores)
	assert.Equal(t, []string{"z-ai/glm-5.2"}, matched)
	assert.Equal(t, []string{"GPT-5"}, unmatched)
}

func TestMatchScoresLowerIsBetter(t *testing.T) {
	t.Parallel()

	board := Board{
		LowerIsBetter: true,
		ModelCol:      0,
		ScoreCol:      1,
		Aliases: map[string]string{
			"glm-5.2": "z-ai/glm-5.2",
		},
	}
	rows := [][]string{
		{"glm-5.2", "3.3"},
		{"glm-5.2 better", "2.1"},
	}

	scores, matched, unmatched := MatchScores(board, rows, []string{"z-ai/glm-5.2"})

	assert.Equal(t, map[string]float64{"z-ai/glm-5.2": 2.1}, scores)
	assert.Equal(t, []string{"z-ai/glm-5.2"}, matched)
	assert.Empty(t, unmatched)
}
