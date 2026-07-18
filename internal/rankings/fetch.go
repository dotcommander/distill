package rankings

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var scorePattern = regexp.MustCompile(`[-+]?[0-9][0-9,]*\.?[0-9]*`)

// ParseMarkdownTableRows extracts data rows from GitHub-style pipe tables in md.
// Each returned row is the list of VISIBLE cell strings (trimmed; the empty
// leading/trailing edge cells produced by leading/trailing pipes are dropped).
// Separator rows (cells consisting only of '-', ':' and spaces) are skipped.
// Header rows are included (harmless - they won't match roster slugs).
func ParseMarkdownTableRows(md string) [][]string {
	var rows [][]string
	for line := range strings.Lines(md) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}

		parts := strings.Split(trimmed, "|")
		if len(parts) >= 2 {
			parts = parts[1 : len(parts)-1]
		}

		row := make([]string, 0, len(parts))
		for _, part := range parts {
			row = append(row, strings.TrimSpace(part))
		}
		if isEmptyRow(row) || isSeparatorRow(row) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// ExtractScore parses the first numeric value from a cell, ignoring %, $, commas
// and surrounding text. Returns ok=false if no number is present.
func ExtractScore(cell string) (float64, bool) {
	match := scorePattern.FindString(cell)
	if match == "" {
		return 0, false
	}
	score, err := strconv.ParseFloat(strings.ReplaceAll(match, ",", ""), 64)
	if err != nil {
		return 0, false
	}
	return score, true
}

// MatchScores maps board rows to roster slugs via board.Aliases (case-insensitive
// substring match on the model column) and returns the score map plus which roster
// slugs matched and which scored rows went unmatched.
// When a slug appears in multiple rows, keep the best per board.LowerIsBetter.
func MatchScores(b Board, rows [][]string, roster []string) (scores map[string]float64, matched []string, unmatched []string) {
	scores = make(map[string]float64)
	rosterSet := make(map[string]struct{}, len(roster))
	for _, slug := range roster {
		rosterSet[strings.ToLower(slug)] = struct{}{}
	}

	aliasKeys := make([]string, 0, len(b.Aliases))
	for alias := range b.Aliases {
		aliasKeys = append(aliasKeys, alias)
	}
	slices.Sort(aliasKeys)

	matchedSet := make(map[string]struct{})
	unmatchedSet := make(map[string]struct{})
	for _, row := range rows {
		if len(row) <= max(b.ModelCol, b.ScoreCol) {
			continue
		}
		modelCell := row[b.ModelCol]
		score, ok := ExtractScore(row[b.ScoreCol])
		if !ok {
			continue
		}

		slug := resolveAlias(modelCell, aliasKeys, b.Aliases)
		if _, ok := rosterSet[strings.ToLower(slug)]; slug != "" && ok {
			if current, exists := scores[slug]; !exists || betterScore(score, current, b.LowerIsBetter) {
				scores[slug] = score
			}
			matchedSet[slug] = struct{}{}
			continue
		}
		unmatchedSet[modelCell] = struct{}{}
	}

	matched = sortedKeys(matchedSet)
	unmatched = sortedKeys(unmatchedSet)
	return scores, matched, unmatched
}

func isEmptyRow(row []string) bool {
	for _, cell := range row {
		if cell != "" {
			return false
		}
	}
	return true
}

func isSeparatorRow(row []string) bool {
	for _, cell := range row {
		if strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return len(row) > 0
}

func resolveAlias(modelCell string, aliasKeys []string, aliases map[string]string) string {
	model := strings.ToLower(modelCell)
	for _, alias := range aliasKeys {
		if strings.Contains(model, strings.ToLower(alias)) {
			return aliases[alias]
		}
	}
	return ""
}

func betterScore(next float64, current float64, lowerIsBetter bool) bool {
	if lowerIsBetter {
		return next < current
	}
	return next > current
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
