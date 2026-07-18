package gradecal

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// RenderPanelHTML renders the de-biased panel tournament as a self-contained
// merit-ranking page with per-judge participation surfaced for auditability.
func RenderPanelHTML(t TournamentResult, panelJudges []string, note string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>distill — merit tournament</title><style>`)
	b.WriteString(`body{font:14px/1.55 -apple-system,system-ui,sans-serif;margin:2rem auto;max-width:1000px;color:#1a1a1a;padding:0 1rem}`)
	b.WriteString(`h1{font-size:1.5rem;margin:0 0 .25rem}p.sub{color:#666;margin:.25rem 0 1rem}`)
	b.WriteString(`table{border-collapse:collapse;width:100%}th,td{padding:.45rem .6rem;border-bottom:1px solid #eee;text-align:left}`)
	b.WriteString(`th{background:#fafafa;font-size:.8rem;text-transform:uppercase;letter-spacing:.03em;color:#555}`)
	b.WriteString(`td.num{text-align:right;font-variant-numeric:tabular-nums}`)
	b.WriteString(`.stats{display:flex;gap:1.5rem;flex-wrap:wrap;margin:1rem 0;padding:.8rem 1rem;background:#f6f8fa;border-radius:8px}`)
	b.WriteString(`.stat b{display:block;font-size:1.3rem}.stat span{color:#666;font-size:.8rem}`)
	b.WriteString(`.good{color:#16a34a}.bad{color:#dc2626}.note{color:#b45309;font-size:.9em}`)
	b.WriteString(`</style></head><body>`)
	b.WriteString("<h1>De-biased panel tournament — leave-one-out merit ranking</h1>")
	fmt.Fprintf(&b, `<p class="sub">De-biased panel run. Note: %s. Panel judges: %s.</p>`, html.EscapeString(note), html.EscapeString(strings.Join(panelJudges, ", ")))

	trust, trustClass := "TRUSTWORTHY", "good"
	if !t.Trustworthy() {
		trust, trustClass = "SUSPECT", "bad"
	}
	b.WriteString(`<div class="stats">`)
	fmt.Fprintf(&b, `<div class="stat"><b class="%s">%s</b><span>verdict</span></div>`, trustClass, trust)
	fmt.Fprintf(&b, `<div class="stat"><b>%d</b><span>comparisons (%d calls)</span></div>`, t.Comparisons, t.Comparisons*2)
	fmt.Fprintf(&b, `<div class="stat"><b class="%s">%.0f%%</b><span>flip rate (want ≤25%%)</span></div>`, okCls(t.FlipRate() <= 0.25), 100*t.FlipRate())
	fmt.Fprintf(&b, `<div class="stat"><b class="%s">%.0f%%</b><span>cycle rate (%d triples, want ≤15%%)</span></div>`, okCls(t.CycleRate() <= 0.15), 100*t.CycleRate(), t.CycleTriples)
	fmt.Fprintf(&b, `<div class="stat"><b>%.0f%%</b><span>grounded reasons</span></div>`, 100*t.GroundedRate())
	if t.Errors > 0 {
		fmt.Fprintf(&b, `<div class="stat"><b class="bad">%d</b><span>judge errors</span></div>`, t.Errors)
	}
	b.WriteString(`</div>`)
	if note != "" {
		b.WriteString(`<p class="note">⚠ ` + html.EscapeString(note) + `</p>`)
	}

	b.WriteString(`<table><thead><tr><th>Rank</th><th>Model</th><th class="num">W</th><th class="num">L</th><th class="num">T</th></tr></thead><tbody>`)
	for i, name := range t.Ranking {
		w := t.Records[name]
		if w == nil {
			w = &WL{}
		}
		fmt.Fprintf(&b, `<tr><td class="num">%d</td><td>%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td></tr>`,
			i+1, html.EscapeString(name), w.Wins, w.Losses, w.Ties)
	}
	b.WriteString(`</tbody></table>`)

	b.WriteString(`<h2 style="font-size:1.1rem;margin-top:1.6rem">Judge participation</h2><table><thead><tr><th>Judge</th><th class="num">Comparisons</th></tr></thead><tbody>`)
	seen := map[string]bool{}
	for _, name := range panelJudges {
		seen[name] = true
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td></tr>`, html.EscapeString(name), t.JudgeCounts[name])
	}
	var extras []string
	for name := range t.JudgeCounts {
		if !seen[name] {
			extras = append(extras, name)
		}
	}
	sort.Strings(extras)
	for _, name := range extras {
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td></tr>`, html.EscapeString(name), t.JudgeCounts[name])
	}
	b.WriteString(`</tbody></table>`)

	if len(t.Edges) > 0 {
		b.WriteString(`<h2 style="font-size:1.1rem;margin-top:1.6rem">Comparisons</h2><table><thead><tr><th>A</th><th>B</th><th>Judge</th><th>Winner</th><th>Judge reason</th></tr></thead><tbody>`)
		for _, e := range t.Edges {
			win := e.Winner
			if e.Flip {
				win = `<span class="bad">flip (tie)</span>`
			} else if win == "" {
				win = "—"
			} else {
				win = html.EscapeString(win)
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				html.EscapeString(e.A), html.EscapeString(e.B), html.EscapeString(e.Judge), win, html.EscapeString(e.Reason))
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
