package app

import (
	"context"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
	"github.com/twsnmp/twearth/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppService provides Wails v3 service bindings for the frontend.
type AppService struct {
	store     *store.Store
	updater   updater.Service
	collector *collector.Manager
	app       *application.App
}

// NewAppService creates the main Wails service instance.
func NewAppService(st *store.Store, upd updater.Service, col *collector.Manager) *AppService {
	return &AppService{
		store:     st,
		updater:   upd,
		collector: col,
	}
}

// SetApp attaches the Wails application instance for event emission.
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
	if s.app != nil {
		s.app.Event.Emit("twearth:config-updated", cfg)
	}
	if s.collector != nil {
		s.collector.TriggerRefresh()
	}
	return nil
}

// GetRecentEvents returns up to limit recent events (newest first).
func (s *AppService) GetRecentEvents(limit int) ([]store.EventRecord, error) {
	return s.store.ListEvents(limit)
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
