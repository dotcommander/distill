package extractscore

import (
	"fmt"
	"html"
	"strings"
)

// okClass returns the CSS class for a boolean pass/fail cell.
func okClass(ok bool) string {
	if ok {
		return "ok"
	}
	return "miss"
}

// RenderDigestHTML renders a self-contained HTML review page for ranked digest
// results. Results are rendered in the order given (the caller ranks them).
// copyPenalty is the weight used for the composite score (recall - penalty*overlap),
// shown so the ranking is reproducible from the page.
func RenderDigestHTML(results []DigestResult, copyPenalty float64) string {
	total := 0
	if len(results) > 0 {
		total = results[0].Total
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>Digest review — write-bakeoff</title><style>`)
	b.WriteString(`body{font:14px/1.55 -apple-system,BlinkMacSystemFont,system-ui,sans-serif;margin:2rem auto;max-width:1100px;color:#1a1a1a;padding:0 1rem}`)
	b.WriteString(`h1{font-size:1.5rem;margin:0 0 .25rem}p.sub{color:#666;margin:.25rem 0 1.25rem}`)
	b.WriteString(`table{border-collapse:collapse;width:100%}`)
	b.WriteString(`th,td{padding:.45rem .6rem;border-bottom:1px solid #eee;text-align:left;vertical-align:top}`)
	b.WriteString(`th{background:#fafafa;position:sticky;top:0;font-size:.8rem;text-transform:uppercase;letter-spacing:.03em;color:#555}`)
	b.WriteString(`td.num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}`)
	b.WriteString(`.bar{height:.55rem;border-radius:3px;background:#eceef1;overflow:hidden;min-width:90px;margin-bottom:.2rem}`)
	b.WriteString(`.bar>span{display:block;height:100%}`)
	b.WriteString(`.flag{color:#b45309;font-size:.85em}.ok{color:#16a34a;font-weight:600}.miss{color:#dc2626;font-weight:600}`)
	b.WriteString(`details{margin:0}summary{cursor:pointer;color:#2563eb;font-size:.85em}`)
	b.WriteString(`code{font-size:.8em;color:#555;word-break:break-word}tr:hover td{background:#fcfcfd}`)
	b.WriteString(`</style></head><body>`)
	fmt.Fprintf(&b, "<h1>Digest review — %d drafts</h1>", len(results))
	fmt.Fprintf(&b, `<p class="sub">Deterministic, no-LLM review. Fact recall over %d golden facts · verbatim copy = fraction of 8-gram shingles lifted from source · tension preservation (3 deliberate discrepancies) · hygiene flags. Ranked by composite = recall &minus; %.1f&times;copy, so copying is punished.</p>`, total, copyPenalty)
	b.WriteString(`<table><thead><tr><th>#</th><th>Model</th><th class="num">Words</th><th>Fact recall</th><th>Copy</th><th class="num">Score</th><th>Tensions</th><th>Flags</th><th>Missing facts</th></tr></thead><tbody>`)
	for i, r := range results {
		pct := 100 * r.Recall()
		color := "#16a34a"
		if pct < 100 {
			color = "#f59e0b"
		}
		if pct < 90 {
			color = "#dc2626"
		}
		tens := fmt.Sprintf(`<span class="%s">%d/%d</span>`, okClass(r.TensionsKept == r.TensionsTotal), r.TensionsKept, r.TensionsTotal)
		if len(r.TensionsMissing) > 0 {
			tens += `<br><code>flat: ` + html.EscapeString(strings.Join(r.TensionsMissing, ", ")) + `</code>`
		}
		var flags []string
		if len(r.Preamble) > 0 {
			flags = append(flags, "preamble:"+strings.Join(r.Preamble, "/"))
		}
		if len(r.Artifacts) > 0 {
			flags = append(flags, "artifact:"+strings.Join(r.Artifacts, "/"))
		}
		if !r.WordBandOK {
			flags = append(flags, fmt.Sprintf("words:%d", r.Words))
		}
		flagHTML := `<span class="ok">clean</span>`
		if len(flags) > 0 {
			flagHTML = `<span class="flag">` + html.EscapeString(strings.Join(flags, "; ")) + `</span>`
		}
		missHTML := `<span class="ok">none</span>`
		if len(r.Missing) > 0 {
			missHTML = fmt.Sprintf(`<details><summary>%d missing</summary><code>%s</code></details>`,
				len(r.Missing), html.EscapeString(strings.Join(r.Missing, ", ")))
		}
		copyPct := 100 * r.Overlap
		copyColor := "#16a34a"
		if copyPct >= 4 {
			copyColor = "#f59e0b"
		}
		if copyPct >= 8 {
			copyColor = "#dc2626"
		}
		score := r.Recall() - copyPenalty*r.Overlap
		fmt.Fprintf(&b,
			`<tr><td class="num">%d</td><td>%s</td><td class="num">%d</td>`+
				`<td><div class="bar"><span style="width:%.0f%%;background:%s"></span></div>%d/%d (%.0f%%)</td>`+
				`<td class="num"><span style="color:%s;font-weight:600">%.1f%%</span></td>`+
				`<td class="num">%.3f</td>`+
				`<td>%s</td><td>%s</td><td>%s</td></tr>`,
			i+1, html.EscapeString(r.Name), r.Words, pct, color, r.Covered, r.Total, pct,
			copyColor, copyPct, score, tens, flagHTML, missHTML)
	}
	b.WriteString(`</tbody></table></body></html>`)
	return b.String()
}
