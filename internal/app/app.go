package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
	"github.com/twsnmp/twearth/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// AppService provides Wails v3 service bindings for the frontend.
type AppService struct {
	store     *store.Store
	updater   updater.Service
	collector *collector.Manager
	app       *application.App

	winMu   sync.Mutex
	windows map[string]*application.WebviewWindow
}

// NewAppService creates the main Wails service instance.
func NewAppService(st *store.Store, upd updater.Service, col *collector.Manager) *AppService {
	return &AppService{
		store:     st,
		updater:   upd,
		collector: col,
		windows:   make(map[string]*application.WebviewWindow),
	}
}

// SetApp attaches the Wails application instance for event emission and window management.
func (s *AppService) SetApp(app *application.App) {
	s.app = app
}

// GetPlatformInfo returns OS, mobile flag, self-update capability, and data directory.
func (s *AppService) GetPlatformInfo() platform.Info {
	return platform.GetInfo()
}

// GetConfig returns the persisted configuration or defaults.
func (s *AppService) GetConfig() (store.Config, error) {
	return s.store.GetConfig()
}

// SaveConfig validates and persists updated configuration.
func (s *AppService) SaveConfig(cfg store.Config) error {
	if err := s.store.SaveConfig(cfg); err != nil {
		return err
	}
	cfg, _ = s.store.GetConfig()
	if s.app != nil {
		s.app.Event.Emit("twearth:config-updated", cfg)
	}
	if s.collector != nil {
		s.collector.TriggerRefresh()
	}
	return nil
}

// GetLatestState returns the most recent cached collector snapshots for immediate hydration.
func (s *AppService) GetLatestState() collector.LatestState {
	if s.collector == nil {
		return collector.LatestState{}
	}
	return s.collector.GetLatestState()
}

// SearchCities queries Open-Meteo Geocoding API for matching city candidates.
func (s *AppService) SearchCities(query, lang string) ([]collector.CitySearchResult, error) {
	if s.collector == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.collector.SearchCities(ctx, query, lang)
}

// DetectCurrentLocation resolves approximate coordinates via IP geolocation as a desktop/fallback candidate.
func (s *AppService) DetectCurrentLocation() (collector.GeoLocationResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return collector.DetectLocationByIP(ctx)
}

// OpenAnalysisWindow opens or focuses a dedicated desktop analysis window ("charts", "synoptic", "marine", "events").
func (s *AppService) OpenAnalysisWindow(view string) error {
	if platform.GetInfo().IsMobile || s.app == nil {
		return fmt.Errorf("multi-window is only supported on desktop")
	}

	titleMap := map[string]string{
		"charts":   "twearth — Multi-Location Analysis",
		"synoptic": "twearth — Synoptic & Upper-Air Analysis",
		"marine":   "twearth — Marine & SST Analysis",
		"events":   "twearth — Event & Hazard Feed",
	}
	title, ok := titleMap[view]
	if !ok {
		return fmt.Errorf("unknown analysis view: %s", view)
	}

	s.winMu.Lock()
	if existing, exists := s.windows[view]; exists && existing != nil {
		s.winMu.Unlock()
		existing.Show()
		existing.Focus()
		return nil
	}
	s.winMu.Unlock()

	width, height := 1040, 720
	if view == "events" {
		width, height = 760, 720
	}

	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            title,
		Width:            width,
		Height:           height,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/?view=" + view,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 46,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	})

	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		s.winMu.Lock()
		delete(s.windows, view)
		s.winMu.Unlock()
	})

	s.winMu.Lock()
	s.windows[view] = win
	s.winMu.Unlock()
	return nil
}

// GetRecentEvents returns up to limit recent events (newest first).
func (s *AppService) GetRecentEvents(limit int) ([]store.EventRecord, error) {
	return s.store.ListEvents(limit)
}

// GetEventsSince returns events within the past hours (or all events if hours <= 0), newest first.
func (s *AppService) GetEventsSince(hours int, limit int) ([]store.EventRecord, error) {
	var since time.Time
	if hours > 0 {
		since = time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	}
	return s.store.ListEventsSince(since, limit)
}

// GetTimeSeries returns snapshots between fromKey and toKey (YYYYMMDDHHmm).
func (s *AppService) GetTimeSeries(fromKey, toKey string) ([]store.TimeSeriesPoint, error) {
	return s.store.ListTimeSeries(fromKey, toKey)
}

// GetUpdaterStatus returns the current self-update status (unsupported on mobile).
func (s *AppService) GetUpdaterStatus() updater.Status {
	return s.updater.GetStatus()
}

// CheckForUpdate checks GitHub Releases for a newer desktop version.
func (s *AppService) CheckForUpdate() updater.Status {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.updater.Check(ctx)
}

// DownloadAndInstallUpdate downloads, verifies, and installs the available desktop update.
func (s *AppService) DownloadAndInstallUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return s.updater.DownloadAndInstall(ctx)
}

// RestartAfterUpdate restarts the desktop application into the newly installed version.
func (s *AppService) RestartAfterUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.updater.Restart(ctx)
}

// NotifyAppForeground is called by the frontend when a mobile app returns to foreground.
func (s *AppService) NotifyAppForeground() {
	if s.collector != nil {
		s.collector.TriggerRefresh()
	}
	if s.app != nil {
		s.app.Event.Emit("twearth:foreground-resumed", time.Now().UTC().Format(time.RFC3339))
	}
}

// RefreshNow triggers an immediate data collection cycle.
func (s *AppService) RefreshNow() {
	if s.collector != nil {
		s.collector.TriggerRefresh()
	}
}
