package labelscore

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// RenderHTML renders a self-contained routing table: models ranked by macro-F1,
// with accuracy, macro-F1, per-class F1, out-of-taxonomy/unparseable counts, and
// (when any model supplies cost) a $/MTok column. task labels the page; scores
// must already be ranked (call RankByMacroF1 first).
func RenderHTML(task string, allowed []string, scores []ModelScore) string {
	showCost := false
	for _, s := range scores {
		if s.CostPerMTok > 0 {
			showCost = true
			break
		}
	}
	classes := append([]string(nil), allowed...)
	sort.Strings(classes)

	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>distill — label routing</title><style>`)
	b.WriteString(`body{font:14px/1.55 -apple-system,system-ui,sans-serif;margin:2rem auto;max-width:1100px;color:#1a1a1a;padding:0 1rem}`)
	b.WriteString(`h1{font-size:1.5rem;margin:0 0 .25rem}p.sub{color:#666;margin:.25rem 0 1rem}`)
	b.WriteString(`table{border-collapse:collapse;width:100%}th,td{padding:.45rem .6rem;border-bottom:1px solid #eee;text-align:left}`)
	b.WriteString(`th{background:#fafafa;font-size:.8rem;text-transform:uppercase;letter-spacing:.03em;color:#555}`)
	b.WriteString(`td.num{text-align:right;font-variant-numeric:tabular-nums}`)
	b.WriteString(`.stats{display:flex;gap:1.5rem;flex-wrap:wrap;margin:1rem 0;padding:.8rem 1rem;background:#f6f8fa;border-radius:8px}`)
	b.WriteString(`.stat b{display:block;font-size:1.3rem}.stat span{color:#666;font-size:.8rem}`)
	b.WriteString(`.good{color:#16a34a}.bad{color:#dc2626}`)
	b.WriteString(`</style></head><body>`)
	b.WriteString("<h1>Label routing — best macro-F1 wins</h1>")
	fmt.Fprintf(&b, `<p class="sub">Task: <b>%s</b>. Deterministic exact-match scoring against gold labels (no LLM judge). Allowed: %s.</p>`,
		html.EscapeString(task), html.EscapeString(strings.Join(classes, ", ")))

	if len(scores) > 0 {
		top := scores[0]
		b.WriteString(`<div class="stats">`)
		fmt.Fprintf(&b, `<div class="stat"><b class="good">%s</b><span>top model</span></div>`, html.EscapeString(top.Model))
		fmt.Fprintf(&b, `<div class="stat"><b>%.3f</b><span>macro-F1</span></div>`, top.MacroF1)
		fmt.Fprintf(&b, `<div class="stat"><b>%.1f%%</b><span>accuracy</span></div>`, 100*top.Accuracy)
		fmt.Fprintf(&b, `<div class="stat"><b>%d</b><span>items</span></div>`, top.N)
		b.WriteString(`</div>`)
	}

	b.WriteString(`<table><thead><tr><th>Rank</th><th>Model</th><th class="num">Macro-F1</th><th class="num">Accuracy</th>`)
	for _, c := range classes {
		b.WriteString(`<th class="num">F1 ` + html.EscapeString(c) + `</th>`)
	}
	b.WriteString(`<th class="num">Out-of-tax</th><th class="num">Unparseable</th><th class="num">ms/item</th>`)
	if showCost {
		b.WriteString(`<th class="num">$/MTok</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)

	for i, s := range scores {
		fmt.Fprintf(&b, `<tr><td class="num">%d</td><td>%s</td><td class="num">%.3f</td><td class="num">%.1f%%</td>`,
			i+1, html.EscapeString(s.Model), s.MacroF1, 100*s.Accuracy)
		f1 := map[string]float64{}
		for _, cm := range s.PerClass {
			f1[cm.Label] = cm.F1
		}
		for _, c := range classes {
			fmt.Fprintf(&b, `<td class="num">%.3f</td>`, f1[c])
		}
		ootCls := "good"
		if s.OutOfVocab > 0 {
			ootCls = "bad"
		}
		perItem := 0.0
		if s.N > 0 {
			perItem = float64(s.ElapsedMS) / float64(s.N)
		}
		fmt.Fprintf(&b, `<td class="num %s">%d</td><td class="num">%d</td><td class="num">%.0f</td>`, ootCls, s.OutOfVocab, s.Unparseable, perItem)
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
