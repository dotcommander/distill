package extractscore

import (
	"fmt"
	"strings"
)

// GateConfig holds the deterministic thresholds that separate tournament-worthy
// candidates from losers/failures/non-candidates, so the paid merit judge never
// revisits them. A zero-value field disables that check.
type GateConfig struct {
	MinRecall       float64 `json:"minRecall"`       // drop low fact recall
	MaxOverlap      float64 `json:"maxOverlap"`      // drop verbatim copiers
	MinTensions     int     `json:"minTensions"`     // drop digests that flatten the discrepancies
	RequireWordBand bool    `json:"requireWordBand"` // drop out-of-band lengths
	RequireClean    bool    `json:"requireClean"`    // drop preamble/artifact hygiene failures
	ExcludeModels   []string `json:"excludeModels"`  // drop models whose name contains any of these substrings (web-search/weak reads), case-insensitive
	MaxFabrications int      `json:"maxFabrications"` // drop digests with MORE THAN this many flagged fabrications (0 disables)
}

// Elimination records why a digest was filtered out of the candidate pool.
type Elimination struct {
	Name    string
	Reasons []string
}

// Gate splits scored digests into candidates (worth paid merit judging) and
// eliminations (with human-readable reasons), preserving input order so a
// composite-sorted slice yields composite-ordered candidates.
func Gate(results []DigestResult, cfg GateConfig) (candidates []DigestResult, eliminated []Elimination) {
	for _, r := range results {
		if reasons := gateReasons(r, cfg); len(reasons) == 0 {
			candidates = append(candidates, r)
		} else {
			eliminated = append(eliminated, Elimination{Name: r.Name, Reasons: reasons})
		}
	}
	return candidates, eliminated
}

// gateReasons returns the human-readable reasons a digest is eliminated from the
// paid-judge pool, or nil if it passes every configured threshold.
func gateReasons(r DigestResult, cfg GateConfig) []string {
	var reasons []string
	if cfg.MinRecall > 0 && r.Recall() < cfg.MinRecall {
		reasons = append(reasons, fmt.Sprintf("low recall %.0f%% < %.0f%%", 100*r.Recall(), 100*cfg.MinRecall))
	}
	if cfg.MaxOverlap > 0 && r.Overlap > cfg.MaxOverlap {
		reasons = append(reasons, fmt.Sprintf("copier %.1f%% > %.1f%%", 100*r.Overlap, 100*cfg.MaxOverlap))
	}
	if cfg.MinTensions > 0 && r.TensionsKept < cfg.MinTensions {
		reasons = append(reasons, fmt.Sprintf("flattened tensions %d/%d", r.TensionsKept, r.TensionsTotal))
	}
	if cfg.RequireClean && (len(r.Preamble) > 0 || len(r.Artifacts) > 0) {
		reasons = append(reasons, "hygiene: preamble/artifact")
	}
	if cfg.RequireWordBand && !r.WordBandOK {
		reasons = append(reasons, fmt.Sprintf("word band %d", r.Words))
	}
	if cfg.MaxFabrications > 0 && len(r.Fabrications) > cfg.MaxFabrications {
		reasons = append(reasons, fmt.Sprintf("fabricates %d claims > %d", len(r.Fabrications), cfg.MaxFabrications))
	}
	for _, ex := range cfg.ExcludeModels {
		if ex != "" && strings.Contains(strings.ToLower(r.Name), strings.ToLower(ex)) {
			reasons = append(reasons, "excluded model (config): matches "+ex)
			break
		}
	}
	return reasons
}
