package gradecal

import (
	"context"
	"math/rand"
	"sync"
)

// WL is a model's win/loss/tie record over the comparisons actually made.
type WL struct{ Wins, Losses, Ties int }

// ComparedEdge records one decided matchup for the report.
type ComparedEdge struct {
	A, B   string
	Winner string // name, or "" for an uncertain (order-flipped) edge
	Flip   bool
	Reason string
	Judge  string // panel member that ruled this comparison
}

// TournamentResult is a merit ranking plus the trust stats that say how much to
// believe it.
type TournamentResult struct {
	Ranking      []string       // best -> worst
	Records      map[string]*WL // per-model record over comparisons made
	Edges        []ComparedEdge // every comparison made
	Comparisons  int            // distinct pairs judged (each = 2 calls)
	Flips        int            // edges where the judge disagreed across slot order
	Errors       int            // comparisons that hit a judge error
	GroundedHits int            // edges with at least one grounded verdict
	CycleTriples int            // strict triples audited for transitivity
	Cycles       int            // intransitive (A>B>C>A) triples found
	JudgeCounts  map[string]int // per-judge comparisons ruled
}

// FlipRate is the fraction of comparisons the judge could not make order-robustly.
func (t TournamentResult) FlipRate() float64 {
	if t.Comparisons == 0 {
		return 0
	}
	return float64(t.Flips) / float64(t.Comparisons)
}

// CycleRate is the fraction of audited triples that were intransitive (guessing
// ≈ 25%); low means the pairwise verdicts form a coherent order.
func (t TournamentResult) CycleRate() float64 {
	if t.CycleTriples == 0 {
		return 0
	}
	return float64(t.Cycles) / float64(t.CycleTriples)
}

// GroundedRate is the fraction of decided edges with at least one grounded reason.
func (t TournamentResult) GroundedRate() float64 {
	if t.Comparisons == 0 {
		return 0
	}
	return float64(t.GroundedHits) / float64(t.Comparisons)
}

// ErrorRate is the fraction of comparisons that hit a judge-call error (bad key,
// a hung judge, persistent parse failures). A high rate means the ranking rests
// on too few real verdicts to trust.
func (t TournamentResult) ErrorRate() float64 {
	if t.Comparisons == 0 {
		return 0
	}
	return float64(t.Errors) / float64(t.Comparisons)
}

// Trustworthy reports whether the ranking is coherent enough to believe: few
// judge-call errors, few order flips, and few transitivity cycles.
func (t TournamentResult) Trustworthy() bool {
	return t.Comparisons > 0 && t.ErrorRate() <= 0.15 && t.FlipRate() <= 0.25 && t.CycleRate() <= 0.15
}

type judgeResolver func(a, b string) (Completer, string)

type comparator struct {
	ctx       context.Context
	resolve   judgeResolver
	r         Renderer
	source    string
	criterion string // "" = merit/faithfulness; "comedy" = funnier-bit
	text      map[string]string
	cache     map[string]string // unordered-pair key -> winner ("" = tie)
	res       *TournamentResult
}

func pairKey(a, b string) string {
	if a < b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

// compare judges a vs b in BOTH slot orders; a decisive winner must win both.
// Disagreement (flip) yields a tie. Results are memoized per unordered pair.
func (c *comparator) compare(a, b string) string {
	if a == b {
		return ""
	}
	k := pairKey(a, b)
	if w, ok := c.cache[k]; ok {
		return w
	}
	j, label := c.resolve(a, b)
	// Forward and reverse slot orders are independent judge calls (distinct
	// return vars, no shared writes); run them concurrently so each comparison
	// waits one call-latency instead of two. State mutation below stays
	// single-threaded after both finish, so the result is identical.
	var vf, vr verdict
	var ef, er error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		vf, ef = judgeOnce(c.ctx, j, c.r, c.criterion, c.source, c.text[a], c.text[b])
	}()
	go func() {
		defer wg.Done()
		vr, er = judgeOnce(c.ctx, j, c.r, c.criterion, c.source, c.text[b], c.text[a])
	}()
	wg.Wait()
	c.res.Comparisons++
	if c.res.JudgeCounts == nil {
		c.res.JudgeCounts = map[string]int{}
	}
	c.res.JudgeCounts[label]++
	edge := ComparedEdge{A: a, B: b}
	edge.Judge = label
	if ef != nil || er != nil {
		c.res.Errors++
		c.cache[k] = ""
		c.res.Edges = append(c.res.Edges, edge)
		return ""
	}
	fwdPick := pickText(vf.Winner, a, b) // forward: slot A=a
	revPick := pickText(vr.Winner, b, a) // reverse: slot A=b
	if groundedInWinner(vf.Reason, c.text[fwdPick]) || groundedInWinner(vr.Reason, c.text[revPick]) {
		c.res.GroundedHits++
	}
	winner := ""
	if fwdPick == revPick {
		winner = fwdPick
	} else {
		c.res.Flips++
		edge.Flip = true
	}
	edge.Winner = winner
	edge.Reason = vf.Reason
	c.res.Edges = append(c.res.Edges, edge)
	c.cache[k] = winner
	c.record(a, b, winner)
	return winner
}

func (c *comparator) record(a, b, winner string) {
	ra, rb := c.res.rec(a), c.res.rec(b)
	switch winner {
	case a:
		ra.Wins++
		rb.Losses++
	case b:
		rb.Wins++
		ra.Losses++
	default:
		ra.Ties++
		rb.Ties++
	}
}

func (t *TournamentResult) rec(name string) *WL {
	if t.Records[name] == nil {
		t.Records[name] = &WL{}
	}
	return t.Records[name]
}

// mergeSort orders names best-first; a tie keeps the seed (left) order, so the
// composite-seeded input breaks ties deterministically.
func (c *comparator) mergeSort(xs []string) []string {
	if len(xs) <= 1 {
		return xs
	}
	mid := len(xs) / 2
	l := c.mergeSort(append([]string(nil), xs[:mid]...))
	r := c.mergeSort(append([]string(nil), xs[mid:]...))
	out := make([]string, 0, len(l)+len(r))
	i, j := 0, 0
	for i < len(l) && j < len(r) {
		if c.compare(l[i], r[j]) == r[j] { // r[j] strictly better -> ranks first
			out = append(out, r[j])
			j++
		} else { // l[i] wins or tie -> stable keeps left first
			out = append(out, l[i])
			i++
		}
	}
	out = append(out, l[i:]...)
	out = append(out, r[j:]...)
	return out
}

// auditCycles samples up to k strict triples from the ranking and counts
// intransitive ones (the direct "is the order coherent or guessed" meter).
func (c *comparator) auditCycles(ranking []string, k int) {
	n := len(ranking)
	if n < 3 || k <= 0 {
		return
	}
	//nolint:gosec // non-crypto: grading-tournament shuffle; cryptographic randomness not required
	rng := rand.New(rand.NewSource(1)) // fixed seed: reproducible audit
	for t := 0; t < k; t++ {
		i, j, l := pick3(rng, n)
		x, y, z := ranking[i], ranking[j], ranking[l]
		wxy := c.compare(x, y)
		wyz := c.compare(y, z)
		wxz := c.compare(x, z)
		if wxy == "" || wyz == "" || wxz == "" {
			continue // need three strict edges to call a cycle
		}
		c.res.CycleTriples++
		if cyclic(x, y, z, wxy, wyz, wxz) {
			c.res.Cycles++
		}
	}
}

func pick3(rng *rand.Rand, n int) (int, int, int) {
	i := rng.Intn(n)
	j := rng.Intn(n)
	for j == i {
		j = rng.Intn(n)
	}
	l := rng.Intn(n)
	for l == i || l == j {
		l = rng.Intn(n)
	}
	return i, j, l
}

// cyclic reports whether the three strict verdicts form a cycle rather than a
// consistent ordering.
func cyclic(x, y, z, wxy, wyz, wxz string) bool {
	beats := map[[2]string]bool{}
	mark := func(w, a, b string) {
		if w == a {
			beats[[2]string{a, b}] = true
		} else {
			beats[[2]string{b, a}] = true
		}
	}
	mark(wxy, x, y)
	mark(wyz, y, z)
	mark(wxz, x, z)
	// A consistent triple has exactly one element that beats both others (a top).
	tops := 0
	for _, a := range []string{x, y, z} {
		others := other2(a, x, y, z)
		if beats[[2]string{a, others[0]}] && beats[[2]string{a, others[1]}] {
			tops++
		}
	}
	return tops != 1
}

func other2(a, x, y, z string) []string {
	var o []string
	for _, v := range []string{x, y, z} {
		if v != a {
			o = append(o, v)
		}
	}
	return o
}

// RunTournament ranks digests by merit via the order-swapped pairwise judge,
// seeding tie-breaks with the input order (pass composite-ranked for stability),
// then audits k triples for transitivity.
func RunTournament(ctx context.Context, judge Completer, r Renderer, source string, digests []Digest, auditTriples int) TournamentResult {
	res := TournamentResult{Records: map[string]*WL{}, JudgeCounts: map[string]int{}}
	c := &comparator{ctx: ctx, resolve: func(_, _ string) (Completer, string) { return judge, "" }, r: r, source: source,
		text: map[string]string{}, cache: map[string]string{}, res: &res}
	names := make([]string, len(digests))
	for i, d := range digests {
		names[i] = d.Name
		c.text[d.Name] = d.Text
	}
	res.Ranking = c.mergeSort(names)
	c.auditCycles(res.Ranking, auditTriples)
	return res
}
