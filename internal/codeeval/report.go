package codeeval

import (
	"fmt"
	"html"
	"strings"
)

// RenderHTML renders a self-contained routing table: models ranked by pass-rate,
// with solved count, compile-fails, blocked-by-scan, ms/problem latency, and
// (when any model supplies cost) a $/MTok column. scores must already be ranked
// (call RankByPassRate first).
func RenderHTML(scores []ModelScore) string {
	showCost := false
	for _, s := range scores {
		if s.CostPerMTok > 0 {
			showCost = true
			break
		}
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>distill — code routing</title><style>`)
	b.WriteString(`body{font:14px/1.55 -apple-system,system-ui,sans-serif;margin:2rem auto;max-width:1100px;color:#1a1a1a;padding:0 1rem}`)
	b.WriteString(`h1{font-size:1.5rem;margin:0 0 .25rem}p.sub{color:#666;margin:.25rem 0 1rem}`)
	b.WriteString(`table{border-collapse:collapse;width:100%}th,td{padding:.45rem .6rem;border-bottom:1px solid #eee;text-align:left}`)
	b.WriteString(`th{background:#fafafa;font-size:.8rem;text-transform:uppercase;letter-spacing:.03em;color:#555}`)
	b.WriteString(`td.num{text-align:right;font-variant-numeric:tabular-nums}`)
	b.WriteString(`.stats{display:flex;gap:1.5rem;flex-wrap:wrap;margin:1rem 0;padding:.8rem 1rem;background:#f6f8fa;border-radius:8px}`)
	b.WriteString(`.stat b{display:block;font-size:1.3rem}.stat span{color:#666;font-size:.8rem}`)
	b.WriteString(`.good{color:#16a34a}.bad{color:#dc2626}`)
	b.WriteString(`</style></head><body>`)
	b.WriteString("<h1>Code routing — best pass-rate wins</h1>")
	b.WriteString(`<p class="sub">Deterministic unit-test pass-rate (static-scan + compile + go test, no LLM judge).</p>`)

	if len(scores) > 0 {
		top := scores[0]
		b.WriteString(`<div class="stats">`)
		fmt.Fprintf(&b, `<div class="stat"><b class="good">%s</b><span>top model</span></div>`, html.EscapeString(top.Model))
		fmt.Fprintf(&b, `<div class="stat"><b>%.1f%%</b><span>pass-rate</span></div>`, 100*top.PassRate)
		fmt.Fprintf(&b, `<div class="stat"><b>%d/%d</b><span>solved</span></div>`, top.Solved, top.N)
		b.WriteString(`</div>`)
	}

	b.WriteString(`<table><thead><tr><th>Rank</th><th>Model</th><th class="num">Pass-rate</th><th class="num">Solved</th><th class="num">Compile-fails</th><th class="num">Blocked</th><th class="num">ms/problem</th>`)
	if showCost {
		b.WriteString(`<th class="num">$/MTok</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)

	for i, s := range scores {
		perProblem := 0.0
		if s.N > 0 {
			perProblem = float64(s.ElapsedMS) / float64(s.N)
		}
		cfCls := "good"
		if s.CompileFails > 0 {
			cfCls = "bad"
		}
		fmt.Fprintf(&b, `<tr><td class="num">%d</td><td>%s</td><td class="num">%.1f%%</td><td class="num">%d/%d</td><td class="num %s">%d</td><td class="num">%d</td><td class="num">%.0f</td>`,
			i+1, html.EscapeString(s.Model), 100*s.PassRate, s.Solved, s.N, cfCls, s.CompileFails, s.Blocked, perProblem)
		if showCost {
			if s.CostPerMTok > 0 {
				fmt.Fprintf(&b, `<td class="num">$%.3f</td>`, s.CostPerMTok)
			} else {
				b.WriteString(`<td class="num">—</td>`)
			}
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></body></html>`)
	return b.String()
}
