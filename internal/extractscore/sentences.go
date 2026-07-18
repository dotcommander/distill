package extractscore

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SplitSentences returns conservative sentence-like units for precision checks.
// It ignores Markdown headings and fenced-code contents, treats each bullet as
// one unit, and only splits punctuation when the next non-space rune is
// uppercase. The bias is toward under-splitting so unsupported text is not
// hidden by over-fragmentation.
func SplitSentences(text string) []string {
	var out []string
	var paragraph strings.Builder
	inFence := false
	flushParagraph := func() {
		out = append(out, splitSentenceParagraph(paragraph.String())...)
		paragraph.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flushParagraph()
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			flushParagraph()
			continue
		}
		if isBulletLine(trimmed) {
			flushParagraph()
			out = append(out, trimmed)
			continue
		}
		if paragraph.Len() > 0 {
			paragraph.WriteByte(' ')
		}
		paragraph.WriteString(trimmed)
	}
	flushParagraph()
	return out
}

func splitSentenceParagraph(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.', '!', '?':
			if !isSentenceBoundary(s, i) {
				continue
			}
			part := strings.TrimSpace(s[start : i+1])
			if part != "" {
				out = append(out, part)
			}
			start = nextNonSpaceIndex(s, i+1)
			i = start - 1
		}
	}
	if tail := strings.TrimSpace(s[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}

func isSentenceBoundary(s string, i int) bool {
	if i > 0 && i+1 < len(s) && isDigitByte(s[i-1]) && isDigitByte(s[i+1]) {
		return false
	}
	next := nextNonSpaceIndex(s, i+1)
	if next >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[next:])
	if !unicode.IsUpper(r) {
		return false
	}
	prevStart := previousTokenStart(s, i)
	prev := strings.Trim(s[prevStart:i], `"'([{`)
	return !isCommonAbbreviation(prev)
}

func nextNonSpaceIndex(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func previousTokenStart(s string, i int) int {
	j := i - 1
	for j >= 0 && !unicode.IsSpace(rune(s[j])) {
		j--
	}
	return j + 1
}

func isCommonAbbreviation(token string) bool {
	switch strings.ToLower(strings.Trim(token, ".")) {
	case "mr", "mrs", "ms", "dr", "prof", "sr", "jr", "st", "vs", "etc", "e.g", "i.e", "u.s", "u.k":
		return true
	default:
		return len(token) == 1 && token != "I"
	}
}

func isBulletLine(s string) bool {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") || strings.HasPrefix(s, "+ ") {
		return true
	}
	if len(s) < 4 || s[0] < '0' || s[0] > '9' {
		return false
	}
	i := 1
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i+1 < len(s) && s[i] == '.' && s[i+1] == ' '
}

func isDigitByte(b byte) bool {
	return b >= '0' && b <= '9'
}
