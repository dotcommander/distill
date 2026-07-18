package gradecal

import (
	"fmt"
	"html"
	"strings"
)

// RenderRecognitionHTML renders a self-contained self-recognition report for
// checking whether judges can identify their own digest above chance.
func RenderRecognitionHTML(results []JudgeRecognition, seed int64, setSize int) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>distill — self-recognition probe</title><style>`)
	b.WriteString(`body{font:14px/1.55 -apple-system,system-ui,sans-serif;margin:2rem auto;max-width:1000px;color:#1a1a1a;padding:0 1rem}`)
	b.WriteString(`h1{font-size:1.5rem;margin:0 0 .25rem}p.sub{color:#666;margin:.25rem 0 1rem}`)
	b.WriteString(`table{border-collapse:collapse;width:100%}th,td{padding:.45rem .6rem;border-bottom:1px solid #eee;text-align:left}`)
	b.WriteString(`th{background:#fafafa;font-size:.8rem;text-transform:uppercase;letter-spacing:.03em;color:#555}`)
	b.WriteString(`td.num{text-align:right;font-variant-numeric:tabular-nums}`)
	b.WriteString(`.stats{display:flex;gap:1.5rem;flex-wrap:wrap;margin:1rem 0;padding:.8rem 1rem;background:#f6f8fa;border-radius:8px}`)
	b.WriteString(`.stat b{display:block;font-size:1.3rem}.stat span{color:#666;font-size:.8rem}`)
	b.WriteString(`.good{color:#16a34a}.bad{color:#dc2626}.note{color:#b45309;font-size:.9em}`)
	b.WriteString(`</style></head><body>`)
	b.WriteString("<h1>Self-recognition probe</h1>")
	b.WriteString(`<p class="sub">Judges choose which anonymized digest in each set is their own. Above-chance recognition means self-preference bias is plausibly real.</p>`)

	trials := 0
	if len(results) > 0 {
		trials = results[0].Trials
	}
	chance := 0.0
	if setSize > 0 {
		chance = 1 / float64(setSize)
	}

	b.WriteString(`<div class="stats">`)
	fmt.Fprintf(&b, `<div class="stat"><b>%d</b><span>judges</span></div>`, len(results))
	fmt.Fprintf(&b, `<div class="stat"><b>%d</b><span>trials per judge</span></div>`, trials)
	fmt.Fprintf(&b, `<div class="stat"><b>%d</b><span>set size</span></div>`, setSize)
	fmt.Fprintf(&b, `<div class="stat"><b>%d</b><span>seed</span></div>`, seed)
	fmt.Fprintf(&b, `<div class="stat"><b>%.0f%%</b><span>chance baseline</span></div>`, 100*chance)
	b.WriteString(`</div>`)

	anyRecognized := false
	for _, r := range results {
		if r.AboveChance() {
			anyRecognized = true
			break
		}
	}

	b.WriteString(`<table><thead><tr><th>Model</th><th class="num">Trials</th><th class="num">Hits</th><th class="num">Accuracy</th><th class="num">Chance</th><th>Verdict</th></tr></thead><tbody>`)
	for _, r := range results {
		cls := okCls(r.AboveChance())
		fmt.Fprintf(&b, `<tr class="%s"><td>%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%.0f%%</td><td class="num">%.0f%%</td><td>%s</td></tr>`,
			cls, html.EscapeString(r.Model), r.Trials, r.Hits, 100*r.Accuracy(), 100*r.Chance(), html.EscapeString(r.Verdict()))
	}
	b.WriteString(`</tbody></table>`)

	if anyRecognized {
		b.WriteString(`<p class="note"><span class="good">Self-recognition present for at least one judge → self-preference bias is plausibly real → de-bias warranted.</span></p>`)
	} else {
		b.WriteString(`<p class="note">All judges at/below chance → self-preference bias is moot → de-bias unnecessary.</p>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
