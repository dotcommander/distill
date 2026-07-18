package transcript

import (
	"regexp"
	"strings"
)

// yt-dlp console-log preamble detection. yt-dlp sometimes prefixes a saved
// transcript with its own progress/telemetry output (subtitle-language dumps,
// download progress, temp .vtt paths). Fed to an extractor, those lines become
// hundreds of junk facts (language codes, byte counts). We strip the maximal
// leading run of machine-emitted yt-dlp log lines, then let the existing
// VTT/SRT/prose cleaning handle the remainder. Detection anchors on yt-dlp
// line shapes (regexes below), never loose keywords, so ordinary prose that
// merely mentions youtube or uses brackets does not false-positive.
var ytdlpLineShapes = []*regexp.Regexp{
	regexp.MustCompile(`^\[youtube\] `),
	regexp.MustCompile(`^\[info\] `),
	regexp.MustCompile(`^\[download\] `),
	regexp.MustCompile(`^\[[a-z][a-z0-9:_-]*\] `),
	regexp.MustCompile(`(?:/|^)subtitle\.[A-Za-z-]+\.vtt`),
}

// isYTDLPLine reports whether a single line matches any yt-dlp log shape.
func isYTDLPLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	for _, re := range ytdlpLineShapes {
		if re.MatchString(t) {
			return true
		}
	}
	return false
}

// hasYTDLPPreamble reports whether data begins with at least two consecutive
// yt-dlp log lines (the two-line floor avoids a single bracketed prose line
// tripping detection). Leading blank lines are skipped.
func hasYTDLPPreamble(data string) bool {
	lines := strings.Split(data, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	matched := 0
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		if !isYTDLPLine(lines[i]) {
			break
		}
		matched++
		if matched >= 2 {
			return true
		}
	}
	return false
}

// stripYTDLPPreamble removes the maximal leading run of yt-dlp log lines and
// returns the remaining body. The yt-dlp progress line frequently concatenates
// the first words of prose onto the trailing download segment; when the final
// preamble line is such a progress line, the prose tail is salvaged. If
// stripping leaves no non-blank content, the original data is returned so the
// caller can fall back to the unmodified input.
func stripYTDLPPreamble(data string) string {
	lines := strings.Split(data, "\n")
	last := -1
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			if nextNonBlankIsYTDLP(lines, i+1) {
				continue
			}
			break
		}
		if !isYTDLPLine(lines[i]) {
			break
		}
		last = i
	}
	if last < 0 {
		return data
	}
	tail := salvageProseTail(lines[last])
	body := strings.Join(lines[last+1:], "\n")
	if tail != "" {
		body = tail + "\n" + body
	}
	return body
}

// nextNonBlankIsYTDLP reports whether the next non-blank line at or after idx is
// a yt-dlp log line.
func nextNonBlankIsYTDLP(lines []string, idx int) bool {
	for ; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "" {
			continue
		}
		return isYTDLPLine(lines[idx])
	}
	return false
}

// downloadDone matches the terminal "[download] 100% of <size> ..." progress
// segment after which yt-dlp prose may be concatenated. The in/at/ETA/frag
// fields are each optional (yt-dlp omits them, or renders the rate as
// "Unknown B/s", depending on source/locale), but every field is typed (sizes,
// times, rates) so the match cannot greedily swallow real prose that happens to
// start with "in"/"at".
var downloadDone = regexp.MustCompile(`\[download\] 100% of\s+~?[0-9.]+[KMGT]?i?B(?:\s+in\s+[0-9:]+)?(?:\s+at\s+(?:[0-9.]+[KMGT]?i?B/s|Unknown B/s))?(?:\s+ETA\s+[0-9:]+)?(?:\s+\(frag\s+\d+/\d+\))?\s*`)

// salvageProseTail extracts prose concatenated after the final download-progress
// segment on a single yt-dlp line. Returns empty string when the line is pure
// telemetry.
func salvageProseTail(line string) string {
	loc := downloadDone.FindAllStringIndex(line, -1)
	if len(loc) == 0 {
		return ""
	}
	return strings.TrimSpace(line[loc[len(loc)-1][1]:])
}
