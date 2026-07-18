package extractscore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// shingleN is the word-shingle width for the verbatim-overlap (copying) signal.
const shingleN = 8

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// Tension is a deliberate unresolved discrepancy in the source. A digest
// preserves it only when every group in Both has at least one alternative
// present — i.e. both sides of the discrepancy survive the rewrite. Each group
// is an any-of list of normalized alternatives.
type Tension struct {
	ID   string     `json:"id"`
	Both [][]string `json:"both"`
}

// Hygiene holds substring patterns that should NOT appear in a clean digest:
// preamble/refusal boilerplate (checked only in the head) and leftover
// pipeline artifacts (checked anywhere).
type Hygiene struct {
	Preamble  []string `json:"preamble"`
	Artifacts []string `json:"artifacts"`
}

// Band is an inclusive word-count range.
type Band struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// DigestChecks is the doc-specific, non-fact review config for a digest.
type DigestChecks struct {
	Tensions []Tension  `json:"tensions"`
	Hygiene  Hygiene    `json:"hygiene"`
	WordBand     Band       `json:"wordBand"`
	Fabrications []string   `json:"fabrications"` // source-specific phrases that are KNOWN fabrications (verified absent from source); any match is an invented claim
	Gate         GateConfig `json:"gate"`
}

// LoadDigestChecks reads a digest-checks.json file.
func LoadDigestChecks(path string) (DigestChecks, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DigestChecks{}, err
	}
	var c DigestChecks
	if err := json.Unmarshal(data, &c); err != nil {
		return DigestChecks{}, fmt.Errorf("extractscore: parsing %s: %w", path, err)
	}
	return c, nil
}

// FlattenFacts loads every expected/chunk-*.json fixture and returns all facts
// in one slice, prefixing each fact id with its chunk so ids stay unique across
// chunks. Used to score a whole-document digest rather than per-chunk extractions.
func FlattenFacts(expectedDir string) ([]Fact, error) {
	matches, err := filepath.Glob(filepath.Join(expectedDir, "chunk-*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var facts []Fact
	for _, p := range matches {
		fx, err := LoadFixture(p)
		if err != nil {
			return nil, err
		}
		for _, f := range fx.Facts {
			f.ID = fx.Chunk + "/" + f.ID
			facts = append(facts, f)
		}
	}
	return facts, nil
}

// DigestResult is the deterministic review of one digest.md.
type DigestResult struct {
	Name            string
	Words           int
	Covered         int
	Total           int
	Missing         []string
	TensionsKept    int
	TensionsTotal   int
	TensionsMissing []string
	Preamble        []string // preamble/refusal patterns found in the head
	Artifacts       []string // pipeline-artifact patterns found anywhere
	Fabrications    []string // fabrication markers found anywhere (invented claims absent from source)
	WordBandOK      bool
	Overlap         float64 // fraction of digest n-gram shingles also in source (verbatim copying)
}

// Recall is Covered/Total (0 when Total == 0).
func (d DigestResult) Recall() float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Covered) / float64(d.Total)
}

// groupMatched reports whether any normalized alternative in the group is
// present in the already-normalized text.
func groupMatched(norm string, group []string) bool {
	for _, alt := range group {
		if containsToken(norm, normalize(alt)) {
			return true
		}
	}
	return false
}

// ScoreDigest runs the deterministic review of one digest: fact recall against
// facts, tension preservation and hygiene against checks, plus the verbatim
// overlap of content against source (a copying signal — high recall earned by
// lifting source text rather than rewriting it).
func ScoreDigest(name, content, source string, facts []Fact, checks DigestChecks) DigestResult {
	norm := normalize(content)
	d := DigestResult{Name: name, Words: len(strings.Fields(content)), Total: len(facts)}
	d.Overlap = shingleOverlap(content, source, shingleN)
	for _, f := range facts {
		if f.covered(norm) {
			d.Covered++
		} else {
			d.Missing = append(d.Missing, f.ID)
		}
	}
	d.TensionsTotal = len(checks.Tensions)
	for _, t := range checks.Tensions {
		kept := true
		for _, group := range t.Both {
			if !groupMatched(norm, group) {
				kept = false
				break
			}
		}
		if kept {
			d.TensionsKept++
		} else {
			d.TensionsMissing = append(d.TensionsMissing, t.ID)
		}
	}
	// Preamble/refusal: only the head of the document, so a legitimate mid-body
	// phrase does not false-positive.
	head := norm
	if len(head) > 400 {
		head = head[:400]
	}
	for _, p := range checks.Hygiene.Preamble {
		if strings.Contains(head, normalize(p)) {
			d.Preamble = append(d.Preamble, p)
		}
	}
	for _, a := range checks.Hygiene.Artifacts {
		if strings.Contains(norm, normalize(a)) {
			d.Artifacts = append(d.Artifacts, a)
		}
	}
	for _, fb := range checks.Fabrications {
		if strings.Contains(norm, normalize(fb)) {
			d.Fabrications = append(d.Fabrications, fb)
		}
	}
	d.WordBandOK = d.Words >= checks.WordBand.Min && d.Words <= checks.WordBand.Max
	return d
}

// WordBandOK reports whether words falls within [lo, hi]. A non-positive lo
// imposes no floor and a non-positive hi no ceiling, so each bound is
// independently optional — WordBandOK(n, 0, 0) is always true. Used by the
// digest command's deterministic quality gate (distinct from ScoreDigest's
// strict, fully-specified band check above).
func WordBandOK(words, lo, hi int) bool {
	if lo > 0 && words < lo {
		return false
	}
	if hi > 0 && words > hi {
		return false
	}
	return true
}

// shingleSet returns the set of n-word shingles (lowercased, alphanumeric words)
// in s.
func shingleSet(s string, n int) map[string]struct{} {
	words := wordRe.FindAllString(strings.ToLower(s), -1)
	set := make(map[string]struct{})
	for i := 0; i+n <= len(words); i++ {
		set[strings.Join(words[i:i+n], " ")] = struct{}{}
	}
	return set
}

// shingleOverlap returns the fraction of content's n-word shingles that also
// appear in source: ~0 means fully rewritten prose, higher means the digest
// lifts runs of words verbatim from the source.
func shingleOverlap(content, source string, n int) float64 {
	cs := shingleSet(content, n)
	if len(cs) == 0 {
		return 0
	}
	ss := shingleSet(source, n)
	hit := 0
	for sh := range cs {
		if _, ok := ss[sh]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(cs))
}
