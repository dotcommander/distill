package rankings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromDefaultRankings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rankings.yaml")
	require.NoError(t, osWriteFile(path, defaultRankings))

	r, err := loadFrom(path)
	require.NoError(t, err)

	assert.Len(t, r.Roster, 12)
	require.Contains(t, r.Boards, "hhem")
	assert.True(t, r.Boards["hhem"].LowerIsBetter)
	assert.Contains(t, r.Boards["hhem"].Scores, "google/gemini-2.5-flash-lite-preview-09-2025")
	assert.Contains(t, r.Boards, "mteb")
	assert.Len(t, r.Roles, 7)
	assert.Equal(t, "write", r.Roles["judge"].CrossFamilyWith)
	assert.Equal(t, "research_model", r.Roles["research"].ConfigKey)
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
