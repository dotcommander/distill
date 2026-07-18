// Package extractscore deterministically scores per-chunk fact extractions
// against golden-fact fixtures. No LLM, no network: coverage (recall) only —
// "of the known key facts in a chunk, how many did the extraction capture?"
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

// Fact is one expected fact. It is covered when every string in Anchors is
// present AND, if Any is non-empty, at least one group has all its strings
// present. Matching is case-insensitive over whitespace-collapsed text.
type Fact struct {
	ID      string     `json:"id"`
	Anchors []string   `json:"anchors,omitempty"`
	Any     [][]string `json:"any,omitempty"`
}

// Fixture is the expected fact set for one chunk.
type Fixture struct {
	Chunk string `json:"chunk"`
	Facts []Fact `json:"facts"`
}

// ChunkResult is the score for one chunk.
type ChunkResult struct {
	Chunk   string
	Covered int
	Total   int
	Missing []string // ids of uncovered facts
}

// Recall returns Covered/Total (0 when Total == 0).
func (r ChunkResult) Recall() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Covered) / float64(r.Total)
}

// RunResult aggregates per-chunk scores for one candidate. Present is the number
// of chunks whose response file existed; Present==0 with len(Chunks)>0 means the
// candidate dir matched no responses (likely a mis-pathed dir, not a bad model).
type RunResult struct {
	Chunks  []ChunkResult
	Covered int
	Total   int
	Present int
}

// Recall is the micro-average: total covered facts / total facts.
func (r RunResult) Recall() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Covered) / float64(r.Total)
}

var (
	wsRe         = regexp.MustCompile(`\s+`)
	digitCommaRe = regexp.MustCompile(`(\d),(\d)`)
)

// normalize lowercases, strips thousands-separator commas that sit between two
// digits (so prose "3,000" matches the "3000" anchor — the digit-boundary guard
// in containsToken still rejects spurious sub-number matches), and collapses
// whitespace runs to single spaces.
func normalize(s string) string {
	s = digitCommaRe.ReplaceAllString(strings.ToLower(s), "$1$2")
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// covered reports whether the normalized extraction satisfies this fact.
func (f Fact) covered(norm string) bool {
	for _, a := range f.Anchors {
		if !containsToken(norm, normalize(a)) {
			return false
		}
	}
	if len(f.Any) == 0 {
		return true
	}
	for _, group := range f.Any {
		all := true
		for _, s := range group {
			if !containsToken(norm, normalize(s)) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// containsToken reports whether anchor appears in norm without being embedded in
// a larger run of the same character class. A digit-edged anchor may not be
// flanked by another digit (or, only when it forms a real decimal, a '.'), so
// "25" does NOT match "2025" and "5.4" does not match "15.4"/"5.45"; a
// letter-edged anchor may not be flanked by a letter, so "pro" does not match
// "product" and "memo" does not match "memory". Matches that ARE preserved:
// unit suffixes ("5.4" in "5.4w", "55" in "55°c"), currency/space prefixes
// ("349" in "$349"), and a trailing sentence period ("349" in "for $349.") —
// because a '.' only extends a number when a digit follows it. Both args are
// already normalized (lowercased, whitespace-collapsed). Byte-level neighbor
// checks are UTF-8 safe: continuation/lead bytes (>=0x80) are never ASCII.
func containsToken(norm, anchor string) bool {
	if anchor == "" {
		return false
	}
	for start := 0; start <= len(norm); {
		idx := strings.Index(norm[start:], anchor)
		if idx < 0 {
			return false
		}
		p := start + idx
		q := p + len(anchor)
		leftOK := p == 0 || !extendsLeft(norm, p, anchor[0])
		rightOK := q == len(norm) || !extendsRight(norm, q, anchor[len(anchor)-1])
		if leftOK && rightOK {
			return true
		}
		start = p + 1
	}
	return false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isLower(b byte) bool { return b >= 'a' && b <= 'z' }

// extendsLeft reports whether the bytes before an anchor at position p in norm
// continue its leading token: a digit edge is extended by a digit, or by a '.'
// ONLY when a digit precedes that '.' (a real fractional part like "0.55"
// before "55" — not an abbreviation dot like "c.1720", where the date 1720 is
// a standalone specific); a letter edge by a letter.
func extendsLeft(norm string, p int, edge byte) bool {
	prev := norm[p-1]
	switch {
	case isDigit(edge):
		if isDigit(prev) {
			return true
		}
		// '.' extends only as a decimal point: a digit must precede it.
		return prev == '.' && p >= 2 && isDigit(norm[p-2])
	case isLower(edge):
		return isLower(prev)
	default:
		return false
	}
}

// extendsRight reports whether the bytes after an anchor (norm[q:]) continue its
// trailing token: a digit edge is extended by a digit, or by a '.' ONLY when a
// digit follows it (a real decimal like "5.45", not a sentence period "$349.");
// a letter edge by a letter.
func extendsRight(norm string, q int, edge byte) bool {
	next := norm[q]
	switch {
	case isDigit(edge):
		if isDigit(next) {
			return true
		}
		return next == '.' && q+1 < len(norm) && isDigit(norm[q+1])
	case isLower(edge):
		return isLower(next)
	default:
		return false
	}
}

// ScoreChunk scores one extraction string against a fixture's facts.
func ScoreChunk(extraction string, facts []Fact) ChunkResult {
	norm := normalize(extraction)
	res := ChunkResult{Total: len(facts)}
	for _, f := range facts {
		if f.covered(norm) {
			res.Covered++
		} else {
			res.Missing = append(res.Missing, f.ID)
		}
	}
	return res
}

// LoadFixture reads and validates one expected/chunk-NNN.json fixture: the chunk
// id must be non-empty and every fact id must be unique and non-empty (a missing
// or duplicate id silently skews that chunk's Total/recall otherwise).
func LoadFixture(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, err
	}
	var fx Fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return Fixture{}, fmt.Errorf("extractscore: parsing %s: %w", path, err)
	}
	if strings.TrimSpace(fx.Chunk) == "" {
		return Fixture{}, fmt.Errorf("extractscore: %s: empty chunk id", path)
	}
	seen := make(map[string]bool, len(fx.Facts))
	for _, f := range fx.Facts {
		if strings.TrimSpace(f.ID) == "" {
			return Fixture{}, fmt.Errorf("extractscore: %s: fact with empty id", path)
		}
		if seen[f.ID] {
			return Fixture{}, fmt.Errorf("extractscore: %s: duplicate fact id %q", path, f.ID)
		}
		seen[f.ID] = true
	}
	return fx, nil
}

// ScoreRun scores a candidate's per-chunk responses against the expected
// fixtures in expectedDir. For each chunk-*.json fixture it reads the matching
// <responsesDir>/<chunk>.md (a missing file counts as an empty extraction, so a
// model that skipped a chunk scores zero for it rather than erroring).
func ScoreRun(expectedDir, responsesDir string) (RunResult, error) {
	matches, err := filepath.Glob(filepath.Join(expectedDir, "chunk-*.json"))
	if err != nil {
		return RunResult{}, err
	}
	sort.Strings(matches)
	var run RunResult
	for _, fxPath := range matches {
		fx, err := LoadFixture(fxPath)
		if err != nil {
			return RunResult{}, err
		}
		extraction, readErr := os.ReadFile(filepath.Join(responsesDir, fx.Chunk+".md"))
		if readErr == nil {
			run.Present++
		}
		cr := ScoreChunk(string(extraction), fx.Facts)
		cr.Chunk = fx.Chunk
		run.Chunks = append(run.Chunks, cr)
		run.Covered += cr.Covered
		run.Total += cr.Total
	}
	// A candidate that matched zero response files is almost certainly a
	// mis-pathed dir, not a model that scored 0 — fail loudly rather than
	// report a misleading recall of 0.000. (A model that skipped SOME chunks
	// still scores those as empty, by design.)
	if len(run.Chunks) > 0 && run.Present == 0 {
		return RunResult{}, fmt.Errorf("extractscore: no response files found in %s for any of %d fixtures (wrong --candidates dir?)", responsesDir, len(run.Chunks))
	}
	return run, nil
}
