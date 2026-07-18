package digest

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/dotcommander/distill/internal/fsutil"
)

func selectTargetFacts(ctx context.Context, embedder BatchEmbedder, facts string, target int, opts Options, ledger *runLedger) (selected string, kept, total int, err error) {
	entries := factEntries(facts)
	total = len(entries)
	if target <= 0 || target >= total {
		return facts, total, total, nil
	}
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.line
	}
	vecs, err := embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return "", 0, total, fmt.Errorf("digest: target-facts embeddings: %w", err)
	}
	selectedIdx := facilityLocationSelect(vecs, target)
	keep := make(map[int]bool, len(selectedIdx))
	for _, idx := range selectedIdx {
		keep[idx] = true
	}
	skip := make(map[int]bool)
	for _, e := range entries {
		if !keep[e.index] {
			skip[e.index] = true
		}
	}
	selected = rebuildFacts(facts, entries, nil, skip)
	kept = len(selectedIdx)
	if opts.ArtifactDir != "" {
		if werr := fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "facts.selected.md"), []byte(selected), 0o644); werr != nil {
			return "", 0, total, fmt.Errorf("digest: writing selected facts: %w", werr)
		}
	}
	if ledger != nil {
		ledger.RecordZeroDelta("select", fmt.Sprintf("kept=%d/%d", kept, total), "apply", time.Time{}, nil)
	}
	return selected, kept, total, nil
}

func facilityLocationSelect(vecs [][]float32, k int) []int {
	n := len(vecs)
	if k <= 0 || n == 0 {
		return nil
	}
	if k >= n {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	selected := make([]int, 0, k)
	chosen := make([]bool, n)
	best := make([]float64, n)
	for len(selected) < k {
		bestIdx := -1
		bestGain := -1.0
		for cand := 0; cand < n; cand++ {
			if chosen[cand] {
				continue
			}
			gain := 0.0
			for i := 0; i < n; i++ {
				sim := cosine(vecs[i], vecs[cand])
				if sim > best[i] {
					gain += sim - best[i]
				}
			}
			if gain > bestGain {
				bestGain = gain
				bestIdx = cand
			}
		}
		if bestIdx < 0 {
			break
		}
		chosen[bestIdx] = true
		selected = append(selected, bestIdx)
		for i := 0; i < n; i++ {
			if sim := cosine(vecs[i], vecs[bestIdx]); sim > best[i] { //nolint:gosec // index provably in-bounds: bestIdx is set from cand loop 0..n-1 with break-if-negative guard
				best[i] = sim
			}
		}
	}
	sort.Ints(selected)
	return selected
}
