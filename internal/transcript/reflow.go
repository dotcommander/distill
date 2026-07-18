package transcript

import "strings"

// Reflow tuning — deterministic grouping knobs, documented Go constants (not
// config: they tune structural grouping, not user-facing behavior). Revisit as
// config only if real tuning demand emerges.
const (
	// paragraphGapSecs starts a new paragraph when the silent gap between
	// consecutive cue start times exceeds this many seconds.
	paragraphGapSecs = 3.0
	// lineWindow starts a new paragraph every N cues when timing is absent.
	lineWindow = 8
)

// isSRT reports whether data looks like SubRip: a numbered cue line immediately
// followed by a timing line. Requires the pair so plain prose with stray
// numbers does not false-positive.
func isSRT(data string) bool {
	lines := strings.Split(data, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if cueNumber.MatchString(strings.TrimSpace(lines[i])) &&
			timingLine.MatchString(strings.TrimSpace(lines[i+1])) {
			return true
		}
	}
	return false
}

// reflow deduplicates the auto-caption triplication / rolling overlap, then
// groups the surviving cues into paragraph blocks separated by blank lines.
func reflow(cues []cue) string {
	deduped := dedupeOverlap(cues)
	var paras []string
	var cur []string
	prevStart := -1.0
	count := 0
	for _, c := range deduped {
		if shouldBreak(c.startSecs, prevStart, count) && len(cur) > 0 {
			paras = append(paras, strings.Join(cur, " "))
			cur = nil
			count = 0
		}
		cur = append(cur, c.text)
		prevStart = c.startSecs
		count++
	}
	if len(cur) > 0 {
		paras = append(paras, strings.Join(cur, " "))
	}
	return strings.Join(paras, "\n\n")
}

// shouldBreak decides a paragraph boundary: a timing gap over the threshold
// when timing is present, else a fixed cue-count window.
func shouldBreak(start, prevStart float64, count int) bool {
	if start >= 0 && prevStart >= 0 {
		return start-prevStart > paragraphGapSecs
	}
	return count >= lineWindow
}

// dedupeOverlap removes consecutive exact-duplicate cues (the triplication bug)
// and rolling overlap where a new cue repeats the tail of the previous one.
// It keeps the longest of an overlapping run so no words are dropped.
func dedupeOverlap(cues []cue) []cue {
	var out []cue
	for _, c := range cues {
		if len(out) == 0 {
			out = append(out, c)
			continue
		}
		last := &out[len(out)-1]
		if c.text == last.text {
			continue // exact duplicate (triplication)
		}
		if merged, ok := mergeOverlap(last.text, c.text); ok {
			last.text = merged
			continue
		}
		out = append(out, c)
	}
	return out
}

// mergeOverlap merges b into a when b's head repeats a's tail (rolling overlap),
// or when one fully contains the other. Returns the merged text and true on a
// detected overlap; false when the cues are independent.
func mergeOverlap(a, b string) (string, bool) {
	if strings.Contains(a, b) {
		return a, true
	}
	if strings.Contains(b, a) {
		return b, true
	}
	aw := strings.Fields(a)
	bw := strings.Fields(b)
	maxN := len(aw)
	if len(bw) < maxN {
		maxN = len(bw)
	}
	for n := maxN; n >= 1; n-- {
		if strings.EqualFold(strings.Join(aw[len(aw)-n:], " "), strings.Join(bw[:n], " ")) {
			return strings.Join(append(aw, bw[n:]...), " "), true
		}
	}
	return "", false
}
