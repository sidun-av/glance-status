package render

import (
	"strings"
	"testing"
)

func TestRenderWidget_ShowsValuesAndArrows(t *testing.T) {
	values := []ValueView{
		{Label: "CPU", ValueText: "42%", HasData: true, ShowArrow: true, ArrowUp: true, ArrowGood: false},
		{Label: "RAM", ValueText: "67%", HasData: true},
		{Label: "DISK (EXT)", HasData: false},
	}
	status := StatusView{Level: "warning", Message: "Warning: DISK (EXT) 91%"}

	html := RenderWidget("Server Health", values, status)

	if !strings.Contains(html, "42%") {
		t.Error("missing CPU value")
	}
	if !strings.Contains(html, `class="stat-arrow stat-arrow-bad"`) {
		t.Error("missing bad/up arrow for CPU (an 'up' arrow gets no extra direction class — only 'down' rotates via stat-arrow-down)")
	}
	if !strings.Contains(html, "no data") {
		t.Error("missing no-data marker for DISK (EXT)")
	}
	if !strings.Contains(html, `data-level="warning"`) {
		t.Error("missing warning status level")
	}
	if !strings.Contains(html, "Warning: DISK (EXT) 91%") {
		t.Error("missing status message")
	}
}

func TestRenderWidget_NoArrowWhenFlagFalse(t *testing.T) {
	values := []ValueView{{Label: "RAM", ValueText: "67%", HasData: true, ShowArrow: false}}
	html := RenderWidget("Server Health", values, StatusView{Level: "ok", Message: "All operational"})
	if strings.Contains(html, "<svg") {
		t.Error("should not render an arrow svg when ShowArrow is false")
	}
}

func TestRenderWidget_EscapesHTML(t *testing.T) {
	status := StatusView{Level: "error", Message: `<script>alert(1)</script>`}
	html := RenderWidget("Server Health", nil, status)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("status message must be HTML-escaped")
	}
}

func TestRenderWidget_StatusLevelsUseThemeColors(t *testing.T) {
	for _, level := range []string{"ok", "warning", "error"} {
		html := RenderWidget("Server Health", nil, StatusView{Level: level, Message: "x"})
		if !strings.Contains(html, `data-level="`+level+`"`) {
			t.Errorf("level %q: missing data-level attribute", level)
		}
	}
	if strings.Contains(RenderWidget("t", nil, StatusView{}), "#") {
		t.Error("must not contain any hardcoded hex color — theme variables only")
	}
}
