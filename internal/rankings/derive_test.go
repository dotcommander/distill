package rankings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankedCandidatesHigherIsBetterAndTieBreak(t *testing.T) {
	t.Parallel()

	roster := []string{"z-ai/glm-5.2", "deepseek/deepseek-v3", "openai/gpt-5"}
	board := Board{
		Metric: "score",
		Scores: map[string]float64{
			"z-ai/glm-5.2":         90,
			"deepseek/deepseek-v3": 95,
			"openai/gpt-5":         80,
		},
	}

	assert.Equal(t, []string{"deepseek/deepseek-v3", "z-ai/glm-5.2", "openai/gpt-5"}, rankedCandidates(board, roster))

	board.Scores["z-ai/glm-5.2"] = 95
	assert.Equal(t, []string{"z-ai/glm-5.2", "deepseek/deepseek-v3", "openai/gpt-5"}, rankedCandidates(board, roster))
	assert.Equal(t, []string{"z-ai/glm-5.2", "deepseek/deepseek-v3", "openai/gpt-5"}, roster)
}

func TestDeriveLowerIsBetterPick(t *testing.T) {
	t.Parallel()

	r := rankingsFixture(
		[]string{"z-ai/glm-5.2", "deepseek/deepseek-v3", "openai/gpt-5"},
		map[string]Board{
			"hhem": {
				Metric:        "hallucination_rate",
				LowerIsBetter: true,
				Scores: map[string]float64{
					"z-ai/glm-5.2":         0.06,
					"deepseek/deepseek-v3": 0.04,
					"openai/gpt-5":         0.08,
				},
			},
		},
		map[string]RoleRule{"judge": {Board: "hhem", ConfigKey: "judge_model"}},
	)

	picks, err := Derive(r)
	require.NoError(t, err)
	require.Len(t, picks, 1)
	assert.Equal(t, "deepseek/deepseek-v3", picks[0].Model)
	assert.Equal(t, "selected", picks[0].Status)
}

func TestDeriveAbsentWhenBoardHasNoRosterModels(t *testing.T) {
	t.Parallel()

	r := rankingsFixture(
		[]string{"z-ai/glm-5.2"},
		map[string]Board{"bench": {Metric: "score", Scores: map[string]float64{"openai/gpt-5": 90}}},
		map[string]RoleRule{"write": {Board: "bench", ConfigKey: "write_model"}},
	)

	picks, err := Derive(r)
	require.NoError(t, err)
	require.Len(t, picks, 1)
	assert.Empty(t, picks[0].Model)
	assert.False(t, picks[0].HasScore)
	assert.Equal(t, "absent", picks[0].Status)
}

func TestDeriveCrossFamilyAdjust(t *testing.T) {
	t.Parallel()

	r := rankingsFixture(
		[]string{"z-ai/glm-5.2", "z-ai/judge-best", "deepseek/deepseek-v3"},
		map[string]Board{
			"writer": {Metric: "score", Scores: map[string]float64{"z-ai/glm-5.2": 99}},
			"judge": {
				Metric: "score",
				Scores: map[string]float64{
					"z-ai/judge-best":      100,
					"deepseek/deepseek-v3": 98,
				},
			},
		},
		map[string]RoleRule{
			"write": {Board: "writer", ConfigKey: "write_model"},
			"judge": {Board: "judge", ConfigKey: "judge_model", CrossFamilyWith: "write"},
		},
	)

	picks, err := Derive(r)
	require.NoError(t, err)
	judge := pickByRole(t, picks, "judge")
	assert.Equal(t, "deepseek/deepseek-v3", judge.Model)
	assert.Equal(t, "cross-family-adjusted", judge.Status)
}

func TestDeriveCrossFamilyAlreadySatisfied(t *testing.T) {
	t.Parallel()

	r := rankingsFixture(
		[]string{"z-ai/glm-5.2", "deepseek/deepseek-v3", "z-ai/judge-next"},
		map[string]Board{
			"writer": {Metric: "score", Scores: map[string]float64{"z-ai/glm-5.2": 99}},
			"judge": {
				Metric: "score",
				Scores: map[string]float64{
					"deepseek/deepseek-v3": 100,
					"z-ai/judge-next":      98,
				},
			},
		},
		map[string]RoleRule{
			"write": {Board: "writer", ConfigKey: "write_model"},
			"judge": {Board: "judge", ConfigKey: "judge_model", CrossFamilyWith: "write"},
		},
	)

	picks, err := Derive(r)
	require.NoError(t, err)
	judge := pickByRole(t, picks, "judge")
	assert.Equal(t, "deepseek/deepseek-v3", judge.Model)
	assert.Equal(t, "selected", judge.Status)
}

func TestDeriveCanonicalOrder(t *testing.T) {
	t.Parallel()

	roles := map[string]RoleRule{
		"embedding": {Board: "bench", ConfigKey: "embedding_model"},
		"judge":     {Board: "bench", ConfigKey: "judge_model"},
		"edit":      {Board: "bench", ConfigKey: "edit_model"},
		"write":     {Board: "bench", ConfigKey: "write_model"},
		"fuse":      {Board: "bench", ConfigKey: "fuse_model"},
		"research":  {Board: "bench", ConfigKey: "research_model"},
		"model":     {Board: "bench", ConfigKey: "model"},
	}
	r := rankingsFixture(
		[]string{"z-ai/glm-5.2"},
		map[string]Board{"bench": {Metric: "score", Scores: map[string]float64{"z-ai/glm-5.2": 1}}},
		roles,
	)

	picks, err := Derive(r)
	require.NoError(t, err)
	assert.Equal(t, []string{"model", "research", "fuse", "write", "edit", "judge", "embedding"}, pickRoles(picks))
}

func rankingsFixture(roster []string, boards map[string]Board, roles map[string]RoleRule) *Rankings {
	return &Rankings{Roster: roster, Boards: boards, Roles: roles}
}

func pickByRole(t *testing.T, picks []Pick, role string) Pick {
	t.Helper()
	for _, pick := range picks {
		if pick.Role == role {
			return pick
		}
	}
	require.Failf(t, "missing pick", "role %q", role)
	return Pick{}
}

func pickRoles(picks []Pick) []string {
	roles := make([]string, 0, len(picks))
	for _, pick := range picks {
		roles = append(roles, pick.Role)
	}
	return roles
}
