// Package gradecal calibrates a pairwise "better read" LLM judge before any
// real merit tournament is run. It pits each intact digest against deterministic
// degradations of itself (planted pairs with a known-correct answer) in both
// slot orders, then reports whether the judge actually discriminates merit or is
// just guessing: swap-robust accuracy on planted pairs and position-bias rate.
package gradecal

import (
	"regexp"
	"strings"
	"unicode"
)

// Sabotage is a deterministic degradation of a digest. It produces a strictly
// worse READ with a known-correct answer (the intact digest should win), so the
// judge's accuracy on it is ground truth, not opinion.
type Sabotage struct {
	Name  string
	Apply func(text string) string
}

// Sabotages returns the standard planted degradations, each attacking a
// different reader-value axis: throughline, completeness, voice/economy, and
// prose-vs-list.
func Sabotages() []Sabotage {
	return []Sabotage{
		{"shuffle", shuffleParas},     // reverse paragraph order -> destroys throughline
		{"truncate", truncateText},    // keep first 45% -> incomplete read
		{"cliche", clicheInject},      // inject AI-beige filler -> bad voice, padding
		{"listify", listify},          // sentences -> bullet dump -> not a read
		{"drop-frame", dropFrame},     // remove first + last sentence -> no hook, no resolution (subtle)
		{"para-reverse", paraReverse}, // reverse sentence order WITHIN each paragraph -> wrecks local logic, keeps structure + facts (subtle)
	}
}

var paraSplit = regexp.MustCompile(`\n\s*\n`)

// shuffleParas reverses paragraph order, wrecking any narrative arc while
// preserving every sentence (so the loss is structure, not facts).
func shuffleParas(t string) string {
	p := paraSplit.Split(strings.TrimSpace(t), -1)
	for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
		p[i], p[j] = p[j], p[i]
	}
	return strings.Join(p, "\n\n")
}

// truncateText keeps the first 45% of words and cuts off mid-thought.
func truncateText(t string) string {
	w := strings.Fields(t)
	n := len(w) * 45 / 100
	if n < 1 {
		n = len(w)
	}
	return strings.Join(w[:n], " ")
}

// clichePrefixes are the filler openers injected by the "cliche" sabotage — the
// definition of that degradation, not user-facing config.
var clichePrefixes = []string{
	"It is important to note that, at the end of the day, ",
	"In today's fast-paced world, one simply cannot deny that ",
	"Needless to say, when all is said and done, ",
}

// clicheInject prepends beige filler to each paragraph, tanking voice and
// signal density without removing any fact.
func clicheInject(t string) string {
	p := paraSplit.Split(strings.TrimSpace(t), -1)
	for i := range p {
		p[i] = clichePrefixes[i%len(clichePrefixes)] + lowerFirst(strings.TrimSpace(p[i]))
	}
	return strings.Join(p, "\n\n")
}

// listify flattens the prose into a verbatim bullet dump — the same facts, none
// of the reading experience.
func listify(t string) string {
	var b strings.Builder
	for _, s := range splitSentences(t) {
		b.WriteString("- ")
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
}

// dropFrame removes the first and last sentence — the hook and the resolution —
// keeping the factual body. A subtle attack on opening/ending craft.
func dropFrame(t string) string {
	s := splitSentences(t)
	if len(s) <= 2 {
		return t
	}
	return strings.Join(s[1:len(s)-1], " ")
}

// paraReverse reverses sentence order within each paragraph: every fact and the
// paragraph structure survive, but intra-paragraph logical flow is wrecked. A
// subtle attack on coherence that a guessing judge will miss.
func paraReverse(t string) string {
	p := paraSplit.Split(strings.TrimSpace(t), -1)
	for i := range p {
		s := splitSentences(p[i])
		for a, b := 0, len(s)-1; a < b; a, b = a+1, b-1 {
			s[a], s[b] = s[b], s[a]
		}
		p[i] = strings.Join(s, " ")
	}
	return strings.Join(p, "\n\n")
}

// splitSentences breaks text on sentence-final punctuation followed by
// whitespace/end. A decimal ("5.4") is not split because the char after the dot
// is a digit, not a space; abbreviations ("Dr.") may over-split, which is
// acceptable for a degradation transform.
func splitSentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '.' && s[i] != '?' && s[i] != '!' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == '"' || s[j] == ')' || s[j] == '\'') {
			j++
		}
		if j >= len(s) || s[j] == ' ' || s[j] == '\n' || s[j] == '\t' {
			if seg := strings.TrimSpace(s[start:j]); seg != "" {
				out = append(out, seg)
			}
			start = j
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// lowerFirst lowercases the first rune so an injected prefix reads as one
// sentence with the original paragraph.
func lowerFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToLower(r)) + s[i+len(string(r)):]
	}
	return s
}
