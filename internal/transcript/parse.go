package transcript

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// Structural parsing patterns — logic, not user-tunable config. Each matches a
// fixed feature of the VTT/SRT grammar or YouTube auto-caption noise.
var (
	// timingLine matches a cue timing line in either VTT or SRT form:
	//   00:00:01.000 --> 00:00:03.500   (VTT, dot ms)
	//   00:00:01,000 --> 00:00:03,500   (SRT, comma ms)
	// plus any trailing cue settings (e.g. "align:start position:0%").
	timingLine = regexp.MustCompile(`^\s*\d{2}:\d{2}:\d{2}[.,]\d{3}\s*-->\s*\d{2}:\d{2}:\d{2}[.,]\d{3}.*$`)
	// cueNumber matches a lone integer line (SRT cue index).
	cueNumber = regexp.MustCompile(`^\s*\d+\s*$`)
	// inlineTS matches an inline word-timing tag: <00:00:00.000>.
	inlineTS = regexp.MustCompile(`<\d{2}:\d{2}:\d{2}[.,]\d{3}>`)
	// styleTag matches VTT styling tags: <c>, </c>, <c.colorClass>, <v Name>.
	styleTag = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	// bracketLabel matches non-speech labels: [Music], [Applause], [Speaker].
	// Structural ("strip bracket labels"), not a tunable allowlist, so it stays
	// a Go constant rather than config data.
	bracketLabel = regexp.MustCompile(`\[[^\]]*\]`)
)

// cue is one parsed caption: its text after noise stripping, plus the start
// time in seconds (-1 when timing was unavailable) used for paragraph gaps.
type cue struct {
	text      string
	startSecs float64
}

// parseCues scans data for cue blocks (VTT or SRT), dropping the WEBVTT header,
// cue numbers, and timing lines, and strips per-cue noise. The current timing
// line's start time is attached to the text lines that follow it.
func parseCues(data string, kind Kind) []cue {
	_ = kind // grammar is unified; kind currently only gates Detect.
	var cues []cue
	sc := bufio.NewScanner(strings.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	curStart := -1.0
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "WEBVTT"):
			continue
		case timingLine.MatchString(trimmed):
			curStart = parseStartSecs(trimmed)
			continue
		case cueNumber.MatchString(trimmed):
			continue
		}
		clean := stripNoise(trimmed)
		if clean == "" {
			continue
		}
		cues = append(cues, cue{text: clean, startSecs: curStart})
	}
	return cues
}

// stripNoise removes inline timestamps, styling tags, and bracket labels from a
// single cue text line, then collapses whitespace.
func stripNoise(s string) string {
	s = inlineTS.ReplaceAllString(s, "")
	s = styleTag.ReplaceAllString(s, "")
	s = bracketLabel.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// parseStartSecs extracts the start time (seconds) from a timing line. Returns
// -1 when the line does not parse, so reflow falls back to the line window.
func parseStartSecs(line string) float64 {
	idx := strings.Index(line, "-->")
	if idx < 0 {
		return -1
	}
	return hmsToSecs(strings.TrimSpace(line[:idx]))
}

// hmsToSecs converts "HH:MM:SS.mmm" or "HH:MM:SS,mmm" to seconds; -1 on error.
func hmsToSecs(s string) float64 {
	s = strings.Replace(s, ",", ".", 1)
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return -1
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return -1
	}
	return float64(h)*3600 + float64(m)*60 + sec
}
