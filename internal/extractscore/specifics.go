package extractscore

import (
	"regexp"
	"strings"
)

// SpecificsResult reports how many specifics (numbers, currency, percentages,
// acronyms, multi-word proper nouns) extracted from a fact set survive into a
// candidate article.
type SpecificsResult struct {
	Covered int      // specifics found in the article
	Total   int      // distinct specifics extracted from the facts
	Missing []string // specifics absent from the article (original form)
}

var (
	// numbers / currency / percent: $1,200  3.5  92%  1977  250M
	specNumRe = regexp.MustCompile(`[$€£]?\d[\d,]*(?:\.\d+)?(?:%|[KMB])?`)
	// acronyms: NASA, JPL, NATO
	specAcronymRe = regexp.MustCompile(`\b[A-Z]{2,}\b`)
	// multi-word proper nouns: Cape Canaveral, New York City
	specProperRe = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+\b`)
	// opaque alphanumeric record IDs shaped like FamilySearch person IDs
	// ("LYQM-85P", "G3BF-N2K", "LCSC-242"): a 4-char and 3-char uppercase
	// alphanumeric pair joined by a hyphen. Their letter/digit fragments are
	// not content facts (the digest keeps the named person, drops the opaque
	// code), so they are stripped before specifics extraction.
	idCodeRe = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{3}\b`)
)

// properLeadStop is the set of capitalized sentence-starter words trimmed off
// the front of a proper-noun match so "The Voyager Probe" reduces to "Voyager
// Probe" and bare "The Voyager" (one real word) is discarded as noise.
var properLeadStop = map[string]bool{
	"the": true, "a": true, "an": true, "this": true, "that": true,
	"these": true, "those": true, "it": true, "in": true, "on": true,
	"at": true, "as": true, "of": true, "and": true, "but": true,
	"or": true, "for": true, "to": true, "from": true, "with": true,
	"by": true, "we": true, "they": true, "he": true, "she": true,
	"his": true, "her": true, "its": true, "their": true,
}

// SpecificsCoverage extracts specifics from factsText and reports which survive
// in articleText, using the same boundary-checked token match as fact scoring
// (so "25" does not match inside "2025"). Deterministic and offline.
func SpecificsCoverage(factsText, articleText string) SpecificsResult {
	// Facts are bullet lines; markdown headings (e.g. the "## chunk-001" scaffolding
	// wrapping each chunk's notes) are not source facts and must not contribute
	// specifics such as "001".
	factsText = dropHeadingLines(factsText)
	factsText = stripIDCodes(factsText)
	seen := map[string]bool{}
	var specifics []string
	add := func(m string) {
		n := normalize(m)
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		specifics = append(specifics, m)
	}
	for _, m := range specNumRe.FindAllString(factsText, -1) {
		if meaningfulNumber(m) {
			add(m)
		}
	}
	for _, m := range specAcronymRe.FindAllString(factsText, -1) {
		add(m)
	}
	for _, m := range specProperRe.FindAllString(factsText, -1) {
		if p := trimProperLead(m); p != "" {
			add(p)
		}
	}

	norm := normalize(articleText)
	res := SpecificsResult{Total: len(specifics)}
	for _, s := range specifics {
		if containsToken(norm, normalize(s)) {
			res.Covered++
		} else {
			res.Missing = append(res.Missing, s)
		}
	}
	return res
}

// meaningfulNumber drops bare single digits (1, 2, 3) that carry no specific
// signal, keeping multi-digit numbers and any currency/percent/decimal form.
func meaningfulNumber(m string) bool {
	core := strings.TrimLeft(m, "$€£")
	return len([]rune(core)) >= 2 || strings.ContainsAny(m, "$€£%")
}

// trimProperLead strips a leading capitalized stopword from a proper-noun
// phrase; it returns "" when fewer than two real words remain.
func trimProperLead(phrase string) string {
	words := strings.Fields(phrase)
	if len(words) > 0 && properLeadStop[strings.ToLower(words[0])] {
		words = words[1:]
	}
	if len(words) < 2 {
		return ""
	}
	return strings.Join(words, " ")
}

// dropHeadingLines removes markdown heading lines (e.g. "## chunk-001") so they
// don't contribute specifics; only fact content (bullets/prose) is scanned.
func dropHeadingLines(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// stripIDCodes blanks opaque alphanumeric record IDs (idCodeRe) that contain a
// letter, so their digit/letter fragments ("85" from "LYQM-85P", "242" from
// "LCSC-242") are not harvested as content specifics. A purely numeric match
// (e.g. a "1234-567" range) is kept, since those can be real data.
func stripIDCodes(s string) string {
	return idCodeRe.ReplaceAllStringFunc(s, func(m string) string {
		if strings.ToLower(m) != m { // contains an uppercase letter -> opaque ID
			return " "
		}
		return m
	})
}
