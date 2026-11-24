package app

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"sort"
	"strconv"

	"github.com/twsnmp/twearth/internal/platform"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const maxAnalysisWindows = 10

type analysisWindowEntry struct {
	id    string
	view  string
	order int
	win   *application.WebviewWindow
}

// AnalysisWindowOptions configures a new desktop analysis window.
type AnalysisWindowOptions struct {
	View          string `json:"view"`
	Mode          string `json:"mode,omitempty"`
	LocationID    string `json:"locationId,omitempty"`
	MarineID      string `json:"marineId,omitempty"`
	RangeKey      string `json:"rangeKey,omitempty"`
	Metric        string `json:"metric,omitempty"`
	Granularity   string `json:"granularity,omitempty"`
	ShowMovingAvg *bool  `json:"showMovingAvg,omitempty"`
	QuakeRadiusKm int    `json:"quakeRadiusKm,omitempty"`
}

var analysisTitleMap = map[string]string{
	"charts":      "twearth — Multi-Location Analysis",
	"synoptic":    "twearth — Synoptic & Upper-Air Analysis",
	"marine":      "twearth — Marine & SST Analysis",
	"events":      "twearth — Event & Hazard Feed",
	"correlation": "twearth — Correlation & ML Analysis",
}

// OpenAnalysisWindow opens a new dedicated desktop analysis window ("charts", "synoptic", "marine", "events", "correlation").
func (s *AppService) OpenAnalysisWindow(view string) error {
	return s.OpenAnalysisWindowWithOptions(AnalysisWindowOptions{View: view})
}

// OpenAnalysisWindowWithOptions opens a new desktop analysis window with optional initial state parameters.
func (s *AppService) OpenAnalysisWindowWithOptions(opts AnalysisWindowOptions) error {
	if platform.GetInfo().IsMobile || s.app == nil {
		return fmt.Errorf("multi-window is only supported on desktop")
	}

	baseTitle, ok := analysisTitleMap[opts.View]
	if !ok {
		return fmt.Errorf("unknown analysis view: %s", opts.View)
	}

	s.winMu.Lock()
	if len(s.windows) >= maxAnalysisWindows {
		s.winMu.Unlock()
		return fmt.Errorf("maximum number of analysis windows (%d) reached", maxAnalysisWindows)
	}
	s.winSeq++
	seq := s.winSeq
	winID := fmt.Sprintf("%s-%d", opts.View, seq)
	openCount := len(s.windows)
	s.winMu.Unlock()

	width, height := 1040, 720
	if opts.View == "events" {
		width, height = 760, 720
	} else if opts.View == "correlation" {
		width, height = 1180, 780
	}

	winOpts := application.WebviewWindowOptions{
		Title:            fmt.Sprintf("%s #%d", baseTitle, seq),
		Width:            width,
		Height:           height,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              buildAnalysisWindowURL(winID, opts),
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 46,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	}

	if area, ok := s.primaryWorkArea(); ok {
		offset := (openCount % 8) * 28
		winOpts.X = area.X + 40 + offset
		winOpts.Y = area.Y + 40 + offset
	}

	win := s.app.Window.NewWithOptions(winOpts)

	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		s.winMu.Lock()
		delete(s.windows, winID)
		s.winMu.Unlock()
	})

	s.winMu.Lock()
	s.windows[winID] = &analysisWindowEntry{
		id:    winID,
		view:  opts.View,
		order: seq,
		win:   win,
	}
	s.winMu.Unlock()
	return nil
}

// SetAnalysisWindowTitle updates the native title bar of a specific analysis window.
func (s *AppService) SetAnalysisWindowTitle(winID, title string) error {
	if winID == "" || title == "" {
		return nil
	}
	s.winMu.Lock()
	entry := s.windows[winID]
	s.winMu.Unlock()
	if entry == nil || entry.win == nil {
		return nil
	}
	entry.win.SetTitle(title)
	return nil
}

// ArrangeAnalysisWindows arranges open analysis subwindows using the specified layout
// ("grid", "horizontal", "vertical", "cascade").
func (s *AppService) ArrangeAnalysisWindows(layout string) error {
	if platform.GetInfo().IsMobile || s.app == nil {
		return fmt.Errorf("multi-window is only supported on desktop")
	}

	s.winMu.Lock()
	entries := make([]*analysisWindowEntry, 0, len(s.windows))
	for _, e := range s.windows {
		if e != nil && e.win != nil {
			entries = append(entries, e)
		}
	}
	s.winMu.Unlock()

	if len(entries) == 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].order < entries[j].order
	})

	area, ok := s.targetWorkArea(entries[0].win)
	if !ok {
		return fmt.Errorf("unable to determine screen work area")
	}

	bounds := computeWindowBounds(layout, len(entries), area)
	if len(bounds) != len(entries) {
		return fmt.Errorf("unknown window layout: %s", layout)
	}

	log.Printf("ArrangeAnalysisWindows layout=%s count=%d workArea=%+v", layout, len(entries), area)
	for i, e := range entries {
		b := bounds[i]
		log.Printf("ArrangeAnalysisWindows[%d] id=%s bounds=%+v", i, e.id, b)
		e.win.Show()
		// Wails macOS SetBounds applies position before size, which miscomputes
		// Cocoa's bottom-left origin.y when height changes. Resize first, then position.
		e.win.SetPosition(b.X, area.Y)
		e.win.SetSize(b.Width, b.Height)
		e.win.SetPosition(b.X, b.Y)
		e.win.Focus()
	}
	return nil
}

// CloseAllAnalysisWindows closes all open analysis windows.
func (s *AppService) CloseAllAnalysisWindows() {
	s.winMu.Lock()
	if len(s.windows) == 0 {
		s.winMu.Unlock()
		return
	}
	wins := make([]*application.WebviewWindow, 0, len(s.windows))
	for k, entry := range s.windows {
		if entry != nil && entry.win != nil {
			wins = append(wins, entry.win)
		}
		delete(s.windows, k)
	}
	s.winMu.Unlock()

	for _, win := range wins {
		win.Close()
	}
}

func (s *AppService) primaryWorkArea() (application.Rect, bool) {
	if s.app == nil || s.app.Screen == nil {
		return application.Rect{}, false
	}
	scr := s.app.Screen.GetPrimary()
	if scr == nil {
		all := s.app.Screen.GetAll()
		if len(all) > 0 {
			scr = all[0]
		}
	}
	if scr == nil {
		return application.Rect{}, false
	}
	return normalizeWorkArea(scr), true
}

func (s *AppService) targetWorkArea(ref *application.WebviewWindow) (application.Rect, bool) {
	if ref != nil {
		if scr, err := ref.GetScreen(); err == nil && scr != nil {
			// On macOS, WebviewWindow.GetScreen() returns pre-scaled coordinates
			// without running ScreenManager's DPI scaling. Resolve the matching
			// screen from ScreenManager so WorkArea is always in DIP points.
			if s.app != nil && s.app.Screen != nil && scr.ID != "" {
				if managed := s.app.Screen.GetByID(scr.ID); managed != nil {
					return normalizeWorkArea(managed), true
				}
			}
			return normalizeWorkArea(scr), true
		}
	}
	return s.primaryWorkArea()
}

func normalizeWorkArea(scr *application.Screen) application.Rect {
	if !scr.WorkArea.IsEmpty() {
		return scr.WorkArea
	}
	if !scr.Bounds.IsEmpty() {
		return scr.Bounds
	}
	return application.Rect{
		X:      scr.X,
		Y:      scr.Y,
		Width:  scr.Size.Width,
		Height: scr.Size.Height,
	}
}

func buildAnalysisWindowURL(winID string, opts AnalysisWindowOptions) string {
	q := url.Values{}
	q.Set("view", opts.View)
	q.Set("winId", winID)
	if opts.Mode != "" {
		q.Set("mode", opts.Mode)
	}
	if opts.LocationID != "" {
		q.Set("loc", opts.LocationID)
	}
	if opts.MarineID != "" {
		q.Set("marineId", opts.MarineID)
	}
	if opts.RangeKey != "" {
		q.Set("range", opts.RangeKey)
	}
	if opts.Metric != "" {
		q.Set("metric", opts.Metric)
	}
	if opts.Granularity != "" {
		q.Set("granularity", opts.Granularity)
	}
	if opts.ShowMovingAvg != nil {
		q.Set("ma", strconv.FormatBool(*opts.ShowMovingAvg))
	}
	if opts.QuakeRadiusKm > 0 {
		q.Set("radius", strconv.Itoa(opts.QuakeRadiusKm))
	} else if opts.Mode == "quake" && opts.QuakeRadiusKm == 0 {
		q.Set("radius", "0")
	}
	return "/?" + q.Encode()
}

func computeWindowBounds(layout string, count int, area application.Rect) []application.Rect {
	if count <= 0 {
		return nil
	}
	if area.Width <= 0 {
		area.Width = 1280
	}
	if area.Height <= 0 {
		area.Height = 800
	}

	switch layout {
	case "grid":
		cols := int(math.Ceil(math.Sqrt(float64(count))))
		rows := (count + cols - 1) / cols
		return tileBounds(count, cols, rows, area)
	case "horizontal":
		return tileBounds(count, count, 1, area)
	case "vertical":
		return tileBounds(count, 1, count, area)
	case "cascade":
		w := min(1040, max(520, area.Width*70/100))
		h := min(720, max(420, area.Height*72/100))
		step := 32
		maxStepsX := max(1, (area.Width-w)/step)
		maxStepsY := max(1, (area.Height-h)/step)
		wrap := max(1, min(maxStepsX, maxStepsY))
		out := make([]application.Rect, count)
		for i := 0; i < count; i++ {
			offset := (i % wrap) * step
			out[i] = application.Rect{
				X:      area.X + offset,
				Y:      area.Y + offset,
				Width:  w,
				Height: h,
			}
		}
		return out
	default:
		return nil
	}
}

func tileBounds(count, cols, rows int, area application.Rect) []application.Rect {
	gap := 8
	if area.Width < cols*320 || area.Height < rows*240 {
		gap = 0
	}
	usableW := max(cols, area.Width-gap*(cols+1))
	usableH := max(rows, area.Height-gap*(rows+1))
	cellW := usableW / cols
	cellH := usableH / rows

	out := make([]application.Rect, count)
	for i := 0; i < count; i++ {
		c := i % cols
		r := i / cols
		w := cellW
		h := cellH
		if c == cols-1 {
			w = area.Width - gap*(cols+1) - cellW*(cols-1)
		}
		if r == rows-1 {
			h = area.Height - gap*(rows+1) - cellH*(rows-1)
		}
		out[i] = application.Rect{
			X:      area.X + gap + c*(cellW+gap),
			Y:      area.Y + gap + r*(cellH+gap),
			Width:  max(200, w),
			Height: max(160, h),
		}
	}
	return out
}
