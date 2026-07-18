package rankings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyToConfigRewritesAndPreservesUnrelatedText(t *testing.T) {
	t.Parallel()

	configText := strings.Join([]string{
		"# keep this header",
		"model: old/shared",
		"write_model: old/x  # stale",
		"judge_model: old/y",
		"unrelated: keep-me  # untouched",
		"# keep this footer",
		"",
	}, "\n")
	picks := []Pick{
		{ConfigKey: "model", Model: "new/shared", Board: "bench", Metric: "score", Score: 99.5, HasScore: true},
		{ConfigKey: "write_model", Model: "new/write", Board: "writer", Metric: "win_rate", Score: 0.75, HasScore: true},
		{ConfigKey: "judge_model", Note: "no roster model on board judge"},
		{ConfigKey: "edit_model", Model: "new/edit", Board: "editor", HasScore: false},
	}

	got, changes := ApplyToConfig(configText, picks)

	require.Len(t, changes, 4)
	assert.Equal(t, Change{ConfigKey: "model", Old: "old/shared", New: "new/shared", Note: "bench score=99.5 (rankings apply)"}, changes[0])
	assert.Equal(t, Change{ConfigKey: "write_model", Old: "old/x", New: "new/write", Note: "writer win_rate=0.75 (rankings apply)"}, changes[1])
	assert.Equal(t, Change{ConfigKey: "judge_model", Note: "no roster model on board judge", Skipped: true}, changes[2])
	assert.Equal(t, Change{ConfigKey: "edit_model", Old: "", New: "new/edit", Note: "editor (rankings apply)"}, changes[3])

	assert.Contains(t, got, "model: new/shared  # bench score=99.5 (rankings apply)\n")
	assert.Contains(t, got, "write_model: new/write  # writer win_rate=0.75 (rankings apply)\n")
	assert.Contains(t, got, "judge_model: old/y\n")
	assert.Contains(t, got, "edit_model: new/edit  # editor (rankings apply)\n")
	assert.Contains(t, got, "# keep this header\n")
	assert.Contains(t, got, "unrelated: keep-me  # untouched\n# keep this footer\n")
	assert.NotContains(t, got, "# stale")
}
