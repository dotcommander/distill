package rankings

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Change records one config.yaml key rewrite (or a skip).
type Change struct {
	ConfigKey string
	Old       string // current value in config text ("" if key absent)
	New       string // chosen model ("" when skipped)
	Note      string // provenance or skip reason
	Skipped   bool   // true when the pick was absent — config left unchanged
}

// ApplyToConfig rewrites per-role keys in configText from picks, preserving the
// rest of the file. It returns the new text and one Change per pick, in pick
// order. Picks with empty Model are skipped and leave configText untouched.
func ApplyToConfig(configText string, picks []Pick) (string, []Change) {
	changes := make([]Change, 0, len(picks))
	out := configText

	for _, pick := range picks {
		if pick.Model == "" {
			changes = append(changes, Change{ConfigKey: pick.ConfigKey, Note: pick.Note, Skipped: true})
			continue
		}

		comment := provenanceComment(pick)
		newLine := fmt.Sprintf("%s: %s  # %s", pick.ConfigKey, pick.Model, comment)
		valueRe := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(pick.ConfigKey) + `:\s*([^\s#]+)`)
		lineRe := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(pick.ConfigKey) + `:.*$`)

		old := ""
		if match := valueRe.FindStringSubmatch(out); len(match) == 2 {
			old = match[1]
		}

		if lineRe.MatchString(out) {
			out = lineRe.ReplaceAllLiteralString(out, newLine)
		} else {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += newLine + "\n"
		}

		changes = append(changes, Change{ConfigKey: pick.ConfigKey, Old: old, New: pick.Model, Note: comment})
	}

	return out, changes
}

func provenanceComment(pick Pick) string {
	if !pick.HasScore {
		return fmt.Sprintf("%s (rankings apply)", pick.Board)
	}
	return fmt.Sprintf("%s %s=%s (rankings apply)", pick.Board, pick.Metric, strconv.FormatFloat(pick.Score, 'g', -1, 64))
}
