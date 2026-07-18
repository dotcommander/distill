package rankings

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Pick is the resolved per-role model selection plus provenance.
type Pick struct {
	Role      string // e.g. "judge"
	ConfigKey string // e.g. "judge_model"
	Model     string // chosen OpenRouter slug; "" if no roster model on the board
	Board     string // board key the pick came from
	Metric    string // board metric (for provenance comments)
	Score     float64
	HasScore  bool
	Status    string // "selected" | "absent" | "cross-family-adjusted"
	Note      string // human-readable provenance / fallback reason
}

var canonicalRoleOrder = []string{"model", "research", "fuse", "write", "edit", "judge", "embedding"}

func family(slug string) string {
	before, _, found := strings.Cut(slug, "/")
	if !found {
		return slug
	}
	return before
}

func rankedCandidates(b Board, roster []string) []string {
	rosterOrder := make(map[string]int, len(roster))
	candidates := make([]string, 0, len(roster))
	for i, model := range roster {
		rosterOrder[model] = i
		if _, ok := b.Scores[model]; ok {
			candidates = append(candidates, model)
		}
	}

	// Ties preserve roster preference regardless of score direction.
	sort.SliceStable(candidates, func(i, j int) bool {
		left := b.Scores[candidates[i]]
		right := b.Scores[candidates[j]]
		if left == right {
			return rosterOrder[candidates[i]] < rosterOrder[candidates[j]]
		}
		if b.LowerIsBetter {
			return left < right
		}
		return left > right
	})

	return candidates
}

func Derive(r *Rankings) ([]Pick, error) {
	if r == nil {
		return nil, errors.New("rankings: nil rankings")
	}

	roles := orderedRoles(r.Roles)
	for _, role := range roles {
		rule := r.Roles[role]
		if rule.CrossFamilyWith == "" {
			continue
		}
		if _, ok := r.Roles[rule.CrossFamilyWith]; !ok {
			return nil, fmt.Errorf("rankings: role %q references missing cross_family_with role %q", role, rule.CrossFamilyWith)
		}
	}

	resolved := make(map[string]Pick, len(roles))

	// Resolve independent roles first so cross-family roles can compare families.
	for _, role := range roles {
		rule := r.Roles[role]
		if rule.CrossFamilyWith != "" {
			continue
		}
		resolved[role] = pickTop(role, rule, r)
	}

	for _, role := range roles {
		rule := r.Roles[role]
		if rule.CrossFamilyWith == "" {
			continue
		}
		resolved[role] = pickCrossFamily(role, rule, r, resolved[rule.CrossFamilyWith])
	}

	picks := make([]Pick, 0, len(roles))
	for _, role := range roles {
		picks = append(picks, resolved[role])
	}
	return picks, nil
}

func orderedRoles(rules map[string]RoleRule) []string {
	roles := make([]string, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for _, role := range canonicalRoleOrder {
		if _, ok := rules[role]; ok {
			roles = append(roles, role)
			seen[role] = true
		}
	}

	extra := make([]string, 0, len(rules))
	for role := range rules {
		if !seen[role] {
			extra = append(extra, role)
		}
	}
	sort.Strings(extra)
	return append(roles, extra...)
}

func pickTop(role string, rule RoleRule, r *Rankings) Pick {
	board, ok := r.Boards[rule.Board]
	if !ok {
		return absentPick(role, rule, fmt.Sprintf("no roster model on board %s", rule.Board))
	}

	candidates := rankedCandidates(board, r.Roster)
	if len(candidates) == 0 {
		return absentPick(role, rule, fmt.Sprintf("no roster model on board %s", rule.Board))
	}
	return selectedPick(role, rule, board, candidates[0], "selected", scoreNote(rule.Board, board, candidates[0]))
}

func pickCrossFamily(role string, rule RoleRule, r *Rankings, ref Pick) Pick {
	board, ok := r.Boards[rule.Board]
	if !ok {
		return absentPick(role, rule, fmt.Sprintf("no roster model on board %s; cross-family constraint with %s could not be applied", rule.Board, rule.CrossFamilyWith))
	}

	candidates := rankedCandidates(board, r.Roster)
	if len(candidates) == 0 {
		return absentPick(role, rule, fmt.Sprintf("no roster model on board %s; cross-family constraint with %s could not be applied", rule.Board, rule.CrossFamilyWith))
	}

	chosen := candidates[0]
	status := "selected"
	note := scoreNote(rule.Board, board, chosen)
	if ref.Model == "" {
		note = fmt.Sprintf("%s; cross-family constraint with %s could not be applied", note, rule.CrossFamilyWith)
		return selectedPick(role, rule, board, chosen, status, note)
	}

	refFamily := family(ref.Model)
	for _, candidate := range candidates {
		if family(candidate) != refFamily {
			chosen = candidate
			break
		}
	}
	if family(chosen) == refFamily {
		note = fmt.Sprintf("%s; no candidate differed in family from %s pick %s", note, rule.CrossFamilyWith, ref.Model)
		return selectedPick(role, rule, board, chosen, status, note)
	}
	if chosen != candidates[0] {
		status = "cross-family-adjusted"
		note = fmt.Sprintf("%s; selected to differ in family from %s pick %s", scoreNote(rule.Board, board, chosen), rule.CrossFamilyWith, ref.Model)
	}
	return selectedPick(role, rule, board, chosen, status, note)
}

func absentPick(role string, rule RoleRule, note string) Pick {
	return Pick{
		Role:      role,
		ConfigKey: rule.ConfigKey,
		Board:     rule.Board,
		Status:    "absent",
		Note:      note,
	}
}

func selectedPick(role string, rule RoleRule, board Board, model string, status string, note string) Pick {
	return Pick{
		Role:      role,
		ConfigKey: rule.ConfigKey,
		Model:     model,
		Board:     rule.Board,
		Metric:    board.Metric,
		Score:     board.Scores[model],
		HasScore:  true,
		Status:    status,
		Note:      note,
	}
}

func scoreNote(boardKey string, board Board, model string) string {
	return fmt.Sprintf("%s %s=%v", boardKey, board.Metric, board.Scores[model])
}
