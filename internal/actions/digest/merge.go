package digest

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/fsutil"
	"github.com/dotcommander/distill/internal/prompts"
)

// BatchEmbedder mirrors the embedding cache interface without coupling digest
// to the cache package.
type BatchEmbedder interface {
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

type mergeResult struct {
	Facts          string
	Contradictions []string
	Clusters       []factCluster
}

type factCluster struct {
	Index    int
	Facts    []string
	Centroid []float32
	Size     int
}

type factEntry struct {
	index int
	line  string
	start int
	end   int
}

func mergeFacts(ctx context.Context, llm Completer, embedder BatchEmbedder, p *prompts.Set, facts string, opts Options, ledger *runLedger) (mergeResult, error) {
	entries := factEntries(facts)
	if len(entries) == 0 {
		return mergeResult{Facts: facts}, nil
	}
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.line
	}
	vecs, err := embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return mergeResult{}, fmt.Errorf("digest: merge embeddings: %w", err)
	}
	if len(vecs) != len(entries) {
		return mergeResult{}, fmt.Errorf("digest: merge embeddings returned %d vectors for %d facts", len(vecs), len(entries))
	}
	clusters := clusterByCosine(vecs, opts.MergeThreshold)
	replacements := make(map[int][]string)
	skip := make(map[int]bool)
	var contradictions []string
	var outlineClusters []factCluster
	clusterID := 0
	for _, cluster := range clusters {
		clusterID++
		cent := centroidForIndices(vecs, cluster)
		if len(cluster) < 2 {
			if opts.MaxSections > 0 {
				outlineClusters = append(outlineClusters, factCluster{
					Index:    clusterID,
					Facts:    []string{entries[cluster[0]].line},
					Centroid: cent,
					Size:     1,
				})
			}
			continue
		}
		input := make([]string, len(cluster))
		for i, idx := range cluster {
			input[i] = entries[idx].line
		}
		started := time.Now()
		beforeUsage := ledger.usageNow()
		out, cerr := complete(ctx, llm, p.RenderMergeFacts(fmt.Sprintf("cluster-%03d", clusterID), strings.Join(input, "\n")), opts.Timeout)
		ledger.Record("merge", fmt.Sprintf("cluster-%03d", clusterID), "call", started, cerr, beforeUsage)
		if cerr != nil {
			return mergeResult{}, fmt.Errorf("digest: merge cluster-%03d: %w", clusterID, cerr)
		}
		parsed := parseMergeOutput(input, out)
		contradictions = append(contradictions, parsed.Contradictions...)
		if len(parsed.Facts) == 0 || !mergePreservesSourceIDs(input, parsed.Facts) {
			outlineClusters = append(outlineClusters, factCluster{Index: clusterID, Facts: input, Centroid: cent, Size: len(cluster)})
			continue
		}
		first := entries[cluster[0]].index
		replacements[first] = parsed.Facts
		for _, idx := range cluster[1:] {
			skip[entries[idx].index] = true
		}
		outlineClusters = append(outlineClusters, factCluster{Index: clusterID, Facts: parsed.Facts, Centroid: cent, Size: len(cluster)})
	}
	merged := rebuildFacts(facts, entries, replacements, skip)
	return mergeResult{Facts: merged, Contradictions: contradictions, Clusters: outlineClusters}, nil
}

type parsedMerge struct {
	Facts          []string
	Contradictions []string
}

func parseMergeOutput(original []string, output string) parsedMerge {
	var parsed parsedMerge
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		t = strings.TrimSpace(t)
		upper := strings.ToUpper(t)
		switch {
		case strings.HasPrefix(upper, "KEEP:"):
			parsed.Facts = append(parsed.Facts, bulletize(strings.TrimSpace(t[len("KEEP:"):])))
		case strings.HasPrefix(upper, "MERGED:"):
			parsed.Facts = append(parsed.Facts, bulletize(strings.TrimSpace(t[len("MERGED:"):])))
		case strings.HasPrefix(upper, "CONTRADICTION:"):
			note := strings.TrimSpace(t[len("CONTRADICTION:"):])
			if note != "" {
				parsed.Contradictions = append(parsed.Contradictions, note)
			}
		}
	}
	if len(parsed.Facts) == 0 && len(original) == 0 {
		return parsedMerge{}
	}
	return parsed
}

func mergePreservesSourceIDs(original, merged []string) bool {
	required := factMarkerSet(original)
	if len(required) == 0 {
		return true
	}
	present := factMarkerSet(merged)
	for id := range required {
		if !present[id] {
			return false
		}
	}
	return true
}

func factMarkerSet(lines []string) map[string]bool {
	ids := map[string]bool{}
	for _, line := range lines {
		for _, group := range citeGroupRe.FindAllString(line, -1) {
			for _, id := range parseCitationIDs(group) {
				ids[strconv.Itoa(id)] = true
			}
		}
	}
	return ids
}

func bulletize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if bulletRe.MatchString(s) {
		return s
	}
	return "- " + s
}

func factEntries(compiled string) []factEntry {
	lines := strings.Split(compiled, "\n")
	var entries []factEntry
	cur := -1
	for i, ln := range lines {
		if bulletRe.MatchString(ln) {
			entries = append(entries, factEntry{index: len(entries), line: ln, start: i, end: i + 1})
			cur = len(entries) - 1
			continue
		}
		if cur >= 0 && strings.TrimSpace(ln) != "" && (strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t")) {
			entries[cur].line += "\n" + ln
			entries[cur].end = i + 1
			continue
		}
		cur = -1
	}
	return entries
}

func rebuildFacts(compiled string, entries []factEntry, replacements map[int][]string, skip map[int]bool) string {
	lines := strings.Split(compiled, "\n")
	starts := make(map[int]factEntry, len(entries))
	continuations := make(map[int]bool)
	for _, e := range entries {
		starts[e.start] = e
		for i := e.start + 1; i < e.end; i++ {
			continuations[i] = true
		}
	}
	var out []string
	for i, line := range lines {
		if continuations[i] {
			continue
		}
		if e, ok := starts[i]; ok {
			if skip[e.index] {
				continue
			}
			if repl, ok := replacements[e.index]; ok {
				for _, r := range repl {
					if strings.TrimSpace(r) != "" {
						out = append(out, r)
					}
				}
				continue
			}
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func writeMergeArtifacts(opts Options, res mergeResult) error {
	if opts.ArtifactDir == "" {
		return nil
	}
	if err := fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "facts.merged.md"), []byte(res.Facts), 0o644); err != nil {
		return fmt.Errorf("digest: writing merged facts: %w", err)
	}
	if len(res.Contradictions) == 0 {
		return nil
	}
	var b strings.Builder
	for _, c := range res.Contradictions {
		if strings.TrimSpace(c) == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(c))
	}
	return fsutil.WriteFile(filepath.Join(opts.ArtifactDir, "responses", "contradictions.md"), []byte(strings.TrimSpace(b.String())), 0o644)
}

func synthesizeOutlineFromClusters(ctx context.Context, llm Completer, p *prompts.Set, units []factUnit, clusters []factCluster, opts Options, ledger *runLedger) (string, error) {
	if len(clusters) == 0 {
		return "", nil
	}
	minFacts := opts.MinSectionFacts
	if minFacts <= 0 {
		minFacts = 3
	}
	beforeMin := len(clusters)
	clusters = coalesceSmallClusters(clusters, minFacts)
	if beforeMin != len(clusters) {
		slog.InfoContext(ctx, "digest coalesce stub clusters", "before", beforeMin, "after", len(clusters), "min_facts", minFacts)
	}
	if opts.MaxSections > 0 {
		before := len(clusters)
		clusters = coalesceClusters(clusters, opts.MaxSections)
		if before != len(clusters) {
			slog.InfoContext(ctx, "digest coalesce clusters", "before", before, "after", len(clusters))
		}
		before = len(clusters)
		clusters = splitOversizedClusters(clusters, opts.MaxSections, opts.ClusterBalanceFactor)
		if before != len(clusters) {
			slog.InfoContext(ctx, "digest split oversized clusters", "before", before, "after", len(clusters))
		}
	}
	byLine := make(map[string][]int, len(units))
	for _, u := range units {
		key := normalizeFactKey(u.line)
		if key != "" {
			byLine[key] = append(byLine[key], u.id)
		}
	}
	type labeledCluster struct {
		index int
		ids   []int
		facts []string
	}
	var labeled []labeledCluster
	var prompt strings.Builder
	for _, c := range clusters {
		ids := clusterFactIDs(c, byLine)
		if len(ids) == 0 {
			continue
		}
		labeled = append(labeled, labeledCluster{index: c.Index, ids: ids, facts: c.Facts})
		fmt.Fprintf(&prompt, "C%d:\n%s\n\n", c.Index, strings.Join(c.Facts, "\n"))
	}
	if len(labeled) == 0 {
		return "", nil
	}
	started := time.Now()
	beforeUsage := ledger.usageNow()
	out, err := retryComplete(ctx, "cluster-labels", llm, p.RenderClusterLabels(strings.TrimSpace(prompt.String())), opts.Timeout, 1, time.Second)
	ledger.Record("outline", "cluster-labels", "call", started, err, beforeUsage)
	if err != nil {
		return "", fmt.Errorf("digest: cluster labels: %w", err)
	}
	labels := parseClusterLabels(out)
	var b strings.Builder
	b.WriteString("# Digest\n\n")
	for _, c := range labeled {
		title := labels[c.index]
		if title == "" {
			title = fmt.Sprintf("Cluster %d", c.index)
		}
		fmt.Fprintf(&b, "## %s\nFacts:", title)
		for i, id := range c.ids {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, " F%d", id)
		}
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func clusterFactIDs(cluster factCluster, byLine map[string][]int) []int {
	seen := map[int]bool{}
	for _, fact := range cluster.Facts {
		for _, id := range byLine[normalizeFactKey(fact)] {
			seen[id] = true
		}
	}
	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func parseClusterLabels(output string) map[int]string {
	labels := map[int]string{}
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "C") {
			continue
		}
		before, after, ok := strings.Cut(t[1:], ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(before))
		if err != nil {
			continue
		}
		if label := strings.TrimSpace(after); label != "" {
			labels[n] = label
		}
	}
	return labels
}

func centroid(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	out := make([]float32, dim)
	for _, v := range vecs {
		for i, val := range v {
			out[i] += val
		}
	}
	n := float32(len(vecs))
	for i := range out {
		out[i] /= n
	}
	return out
}

func centroidForIndices(vecs [][]float32, indices []int) []float32 {
	sub := make([][]float32, len(indices))
	for i, idx := range indices {
		sub[i] = vecs[idx]
	}
	return centroid(sub)
}

func coalesceSmallClusters(clusters []factCluster, minFacts int) []factCluster {
	if minFacts <= 1 || len(clusters) < 2 {
		return clusters
	}
	result := cloneClusters(clusters)
	for {
		small := -1
		for i, c := range result {
			if c.Size < minFacts {
				small = i
				break
			}
		}
		if small < 0 || len(result) < 2 {
			break
		}
		neighbor := nearestCluster(result, small)
		if neighbor < 0 {
			break
		}
		keep, drop := small, neighbor
		if drop < keep {
			keep, drop = drop, keep
		}
		result[keep] = mergeFactClusters(result[keep], result[drop])
		result = append(result[:drop], result[drop+1:]...)
	}
	for i := range result {
		result[i].Index = i + 1
	}
	return result
}

func nearestCluster(clusters []factCluster, idx int) int {
	best := -1
	bestSim := -2.0
	for i := range clusters {
		if i == idx {
			continue
		}
		sim := cosine(clusters[idx].Centroid, clusters[i].Centroid)
		if sim > bestSim || (sim == bestSim && i < idx && (best > idx || best < 0)) {
			best = i
			bestSim = sim
		}
	}
	return best
}

func splitOversizedClusters(clusters []factCluster, maxSections int, factor float64) []factCluster {
	if maxSections <= 0 || len(clusters) == 0 {
		return clusters
	}
	if factor <= 0 {
		factor = 1.5
	}
	total := 0
	for _, c := range clusters {
		total += c.Size
	}
	if total == 0 {
		return clusters
	}
	capFacts := int(math.Ceil(float64(total) / float64(maxSections) * factor))
	if capFacts < 1 {
		capFacts = 1
	}
	var result []factCluster
	for _, c := range clusters {
		if c.Size <= capFacts || len(c.Facts) < 2 {
			result = append(result, c)
			continue
		}
		for start := 0; start < len(c.Facts); start += capFacts {
			end := start + capFacts
			if end > len(c.Facts) {
				end = len(c.Facts)
			}
			part := factCluster{
				Facts:    append([]string(nil), c.Facts[start:end]...),
				Centroid: append([]float32(nil), c.Centroid...),
				Size:     end - start,
			}
			result = append(result, part)
		}
	}
	for i := range result {
		result[i].Index = i + 1
	}
	return result
}

// coalesceClusters merges clusters by centroid cosine similarity until the
// group count is at most maxSections. Ties break by lowest group index.
// When maxSections is zero or already satisfies the count, clusters is
// returned unchanged. Calling this with maxSections <= 0 produces
// byte-for-byte identical output to the uncapped path on the same input.
func coalesceClusters(clusters []factCluster, maxSections int) []factCluster {
	if maxSections <= 0 || len(clusters) <= maxSections {
		return clusters
	}
	result := cloneClusters(clusters)
	for len(result) > maxSections {
		bestI, bestJ := -1, -1
		bestSim := -1.0
		for i := 0; i < len(result); i++ {
			for j := i + 1; j < len(result); j++ {
				sim := cosine(result[i].Centroid, result[j].Centroid)
				if sim > bestSim {
					bestSim = sim
					bestI, bestJ = i, j
				}
			}
		}
		if bestI < 0 {
			break
		}
		result[bestI] = mergeFactClusters(result[bestI], result[bestJ])
		result = append(result[:bestJ], result[bestJ+1:]...)
	}
	for i := range result {
		result[i].Index = i + 1
	}
	return result
}

func cloneClusters(clusters []factCluster) []factCluster {
	result := make([]factCluster, len(clusters))
	for i, c := range clusters {
		result[i] = c
		result[i].Facts = append([]string(nil), c.Facts...)
		result[i].Centroid = append([]float32(nil), c.Centroid...)
	}
	return result
}

func mergeFactClusters(a, b factCluster) factCluster {
	n1, n2 := a.Size, b.Size
	total := n1 + n2
	a.Facts = append(a.Facts, b.Facts...)
	if len(a.Centroid) == len(b.Centroid) && total > 0 {
		newCentroid := make([]float32, len(a.Centroid))
		for k := range newCentroid {
			newCentroid[k] = float32((float64(a.Centroid[k])*float64(n1) + float64(b.Centroid[k])*float64(n2)) / float64(total))
		}
		a.Centroid = newCentroid
	}
	a.Size = total
	return a
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, aa, bb float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return dot / (math.Sqrt(aa) * math.Sqrt(bb))
}

type dsu struct {
	parent []int
}

func newDSU(n int) *dsu {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	return &dsu{parent: parent}
}

func (d *dsu) find(x int) int {
	if d.parent[x] != x {
		d.parent[x] = d.find(d.parent[x])
	}
	return d.parent[x]
}

func (d *dsu) union(a, b int) {
	ra, rb := d.find(a), d.find(b)
	if ra == rb {
		return
	}
	if rb < ra {
		ra, rb = rb, ra
	}
	d.parent[rb] = ra
}

func clusterByCosine(vecs [][]float32, threshold float64) [][]int {
	if len(vecs) == 0 {
		return nil
	}
	d := newDSU(len(vecs))
	for i := 0; i < len(vecs); i++ {
		for j := i + 1; j < len(vecs); j++ {
			if cosine(vecs[i], vecs[j]) >= threshold {
				d.union(i, j)
			}
		}
	}
	grouped := map[int][]int{}
	for i := range vecs {
		root := d.find(i)
		grouped[root] = append(grouped[root], i)
	}
	clusters := make([][]int, 0, len(grouped))
	for _, members := range grouped {
		sort.Ints(members)
		clusters = append(clusters, members)
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i][0] < clusters[j][0]
	})
	return clusters
}
