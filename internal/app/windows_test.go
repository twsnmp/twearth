package app

import (
	"net/url"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestComputeWindowBounds(t *testing.T) {
	area := application.Rect{X: 100, Y: 50, Width: 1600, Height: 900}

	t.Run("zero count returns nil", func(t *testing.T) {
		if got := computeWindowBounds("grid", 0, area); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("unknown layout returns nil", func(t *testing.T) {
		if got := computeWindowBounds("diagonal", 2, area); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("grid 3 windows uses 2x2", func(t *testing.T) {
		rects := computeWindowBounds("grid", 3, area)
		if len(rects) != 3 {
			t.Fatalf("expected 3 rects, got %d", len(rects))
		}
		// First two on row 0, third on row 1
		if rects[0].Y != rects[1].Y {
			t.Fatalf("expected row 0 Y to match: %d vs %d", rects[0].Y, rects[1].Y)
		}
		if rects[2].Y <= rects[0].Y {
			t.Fatalf("expected row 1 Y > row 0 Y: %d vs %d", rects[2].Y, rects[0].Y)
		}
		for i, r := range rects {
			if r.X < area.X || r.Y < area.Y || r.X+r.Width > area.X+area.Width || r.Y+r.Height > area.Y+area.Height {
				t.Fatalf("rect %d out of area bounds: %+v", i, r)
			}
		}
	})

	t.Run("horizontal splits side by side", func(t *testing.T) {
		rects := computeWindowBounds("horizontal", 3, area)
		if len(rects) != 3 {
			t.Fatalf("expected 3 rects, got %d", len(rects))
		}
		for i := 1; i < len(rects); i++ {
			if rects[i].X <= rects[i-1].X {
				t.Fatalf("expected increasing X: %+v", rects)
			}
			if rects[i].Y != rects[0].Y || rects[i].Height != rects[0].Height {
				t.Fatalf("expected matching Y/Height: %+v", rects)
			}
		}
	})

	t.Run("vertical stacks top to bottom", func(t *testing.T) {
		rects := computeWindowBounds("vertical", 3, area)
		if len(rects) != 3 {
			t.Fatalf("expected 3 rects, got %d", len(rects))
		}
		for i := 1; i < len(rects); i++ {
			if rects[i].Y <= rects[i-1].Y {
				t.Fatalf("expected increasing Y: %+v", rects)
			}
			if rects[i].X != rects[0].X || rects[i].Width != rects[0].Width {
				t.Fatalf("expected matching X/Width: %+v", rects)
			}
		}
	})

	t.Run("cascade offsets diagonally within area", func(t *testing.T) {
		rects := computeWindowBounds("cascade", 4, area)
		if len(rects) != 4 {
			t.Fatalf("expected 4 rects, got %d", len(rects))
		}
		for i, r := range rects {
			if r.X < area.X || r.Y < area.Y || r.X+r.Width > area.X+area.Width || r.Y+r.Height > area.Y+area.Height {
				t.Fatalf("cascade rect %d out of area bounds: %+v", i, r)
			}
			if i > 0 && (r.X <= rects[i-1].X || r.Y <= rects[i-1].Y) {
				t.Fatalf("expected diagonal offset at %d: prev=%+v cur=%+v", i, rects[i-1], r)
			}
		}
	})
}

func TestBuildAnalysisWindowURL(t *testing.T) {
	showMA := false
	raw := buildAnalysisWindowURL("charts-2", AnalysisWindowOptions{
		View:          "charts",
		Mode:          "trend",
		LocationID:    "loc-tokyo",
		RangeKey:      "1y",
		Metric:        "pressureMsl",
		Granularity:   "daily",
		ShowMovingAvg: &showMA,
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("failed to parse url %q: %v", raw, err)
	}
	q := u.Query()
	if q.Get("view") != "charts" || q.Get("winId") != "charts-2" {
		t.Fatalf("unexpected view/winId: %v", q)
	}
	if q.Get("mode") != "trend" || q.Get("loc") != "loc-tokyo" || q.Get("range") != "1y" || q.Get("metric") != "pressureMsl" || q.Get("granularity") != "daily" || q.Get("ma") != "false" {
		t.Fatalf("unexpected query params: %v", q)
	}
}
