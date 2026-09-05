package render

import (
	"fmt"
	"html"
	"strings"
)

type ValueView struct {
	Label     string
	ValueText string
	HasData   bool
	ShowArrow bool
	ArrowUp   bool
	ArrowGood bool
}

type StatusView struct {
	Level   string // "ok", "warning", "error"
	Message string
}

func styleBlock() string {
	return `<style>
	.stat-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(90px,1fr));gap:10px;margin-bottom:12px}
	.stat-tile{display:flex;flex-direction:column;gap:2px}
	.stat-label{font-size:10.5px;color:var(--color-text-subdue);text-transform:uppercase;letter-spacing:.03em}
	.stat-value-row{display:flex;align-items:center;gap:4px}
	.stat-value{font-size:16px;font-weight:600;color:var(--color-text-highlight)}
	.stat-value-nodata{font-size:12px;color:var(--color-text-subdue)}
	.stat-arrow{width:9px;height:9px}
	.stat-arrow-down{transform:rotate(180deg)}
	.stat-arrow-good{color:var(--color-primary)}
	.stat-arrow-bad{color:var(--color-negative)}
	.status-line{display:flex;align-items:center;gap:8px;padding-top:10px;border-top:1px solid var(--color-widget-content-border)}
	.status-dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
	.status-dot[data-level="ok"]{background:var(--color-primary)}
	.status-dot[data-level="error"]{background:var(--color-negative)}
	.status-dot[data-level="warning"]{background:transparent;border:1.5px solid var(--color-negative)}
	.status-text{font-size:12px;color:var(--color-text-base)}
</style>`
}

const arrowSVG = `<svg class="stat-arrow %s" viewBox="0 0 10 10" fill="currentColor"><path d="M5 0 L10 8 L0 8 Z"/></svg>`

// RenderWidget renders the widget body HTML. It does not render a heading
// for title — Glance renders the widget's header chrome itself from the
// Widget-Title response header (see main.go), the same convention followed
// by the sibling repos. The title parameter is kept for signature
// stability even though it is currently unused.
func RenderWidget(title string, values []ValueView, status StatusView) string {
	var b strings.Builder
	b.WriteString(styleBlock())
	b.WriteString(`<div class="stat-body"><div class="stat-grid">`)

	for _, v := range values {
		b.WriteString(`<div class="stat-tile">`)
		fmt.Fprintf(&b, `<span class="stat-label">%s</span>`, html.EscapeString(v.Label))
		b.WriteString(`<span class="stat-value-row">`)
		if v.HasData {
			fmt.Fprintf(&b, `<span class="stat-value">%s</span>`, html.EscapeString(v.ValueText))
			if v.ShowArrow {
				dir := ""
				if !v.ArrowUp {
					dir = "stat-arrow-down"
				}
				color := "stat-arrow-bad"
				if v.ArrowGood {
					color = "stat-arrow-good"
				}
				fmt.Fprintf(&b, arrowSVG, strings.TrimSpace(dir+" "+color))
			}
		} else {
			b.WriteString(`<span class="stat-value-nodata">no data</span>`)
		}
		b.WriteString(`</span></div>`)
	}
	b.WriteString(`</div>`)

	fmt.Fprintf(&b, `<div class="status-line"><span class="status-dot" data-level="%s"></span><span class="status-text">%s</span></div>`,
		html.EscapeString(status.Level), html.EscapeString(status.Message))

	b.WriteString(`</div>`)
	return b.String()
}
