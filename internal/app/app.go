package app

import (
	"context"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/mcp"
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
	mcpSvc    mcp.Service
	app       *application.App
	discovery *analysis.DiscoveryManager

	winMu   sync.Mutex
	winSeq  int
	windows map[string]*analysisWindowEntry
}

// NewAppService creates the main Wails service instance.
func NewAppService(st *store.Store, upd updater.Service, col *collector.Manager) *AppService {
	svc := &AppService{
		store:     st,
		updater:   upd,
		collector: col,
		windows:   make(map[string]*analysisWindowEntry),
	}
	svc.discovery = analysis.NewDiscoveryManager(
		st,
		func(prog analysis.DiscoveryProgress) {
			if svc.app != nil {
				svc.app.Event.Emit("twearth:correlation-discovery-progress", prog)
			}
		},
		func(item analysis.DiscoveryResultItem) {
			if svc.app != nil {
				svc.app.Event.Emit("twearth:correlation-discovery-result", item)
			}
		},
	)
	return svc
}

// SetApp attaches the Wails application instance for event emission and window management.
func (s *AppService) SetApp(app *application.App) {
	s.app = app
}

// SetMCPService attaches the local-only MCP server service.
func (s *AppService) SetMCPService(m mcp.Service) {
	s.mcpSvc = m
}

// GetMCPStatus returns the current runtime status of the local-only MCP server.
func (s *AppService) GetMCPStatus() mcp.Status {
	if s.mcpSvc == nil {
		return mcp.Status{}
	}
	return s.mcpSvc.GetStatus()
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
		s.collector.TriggerHistorySync()
	}
	if s.mcpSvc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.mcpSvc.Configure(ctx, cfg.MCPEnabled, cfg.MCPPort)
	}
	return nil
}

// PreviewAutoMarineLocations returns recommended marine locations merged with manual user points
// based on cfg's active primary observation location and climate-region preference.
func (s *AppService) PreviewAutoMarineLocations(cfg store.Config) []store.MarineLocation {
	return store.MergeAutoMarineLocations(cfg.MarineLocations, cfg.PrimaryLocation(), cfg.AutoMarineClimate)
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

// QueryHistory retrieves, resamples, and computes statistics for a location's time series.
func (s *AppService) QueryHistory(req store.HistoryQuery) (store.HistoryResult, error) {
	return s.store.QueryHistory(req)
}

// QueryQuakeHistory aggregates stored earthquakes optionally filtered by distance from a coordinate.
func (s *AppService) QueryQuakeHistory(req store.QuakeHistoryQuery) (store.QuakeHistoryResult, error) {
	return s.store.QueryQuakeHistory(req)
}

// GetRecentDisasters returns up to limit persisted disaster records (newest first).
func (s *AppService) GetRecentDisasters(limit int, activeOnly bool) ([]store.DisasterRecord, error) {
	return s.store.ListRecentDisasters(limit, activeOnly)
}

// QueryDisasterHistory analyzes persisted disaster history and nearby natural disaster monuments.
func (s *AppService) QueryDisasterHistory(req store.DisasterHistoryQuery) (store.DisasterHistoryResult, error) {
	return s.store.QueryDisasterHistory(req)
}

// GetLatestRiverStations returns the latest water level observation for each monitored river station.
func (s *AppService) GetLatestRiverStations() ([]store.RiverTimeSeriesPoint, error) {
	return s.store.ListLatestRiverStations()
}

// QueryRiverHistory returns time-series water level history for a specific river station.
func (s *AppService) QueryRiverHistory(req store.RiverHistoryQuery) (store.RiverHistoryResult, error) {
	return s.store.QueryRiverHistory(req)
}

// QueryDisasterLore queries locally cached GSI Natural Disaster Monuments.
func (s *AppService) QueryDisasterLore(req store.DisasterLoreQuery) ([]store.DisasterLoreRecord, error) {
	return s.store.QueryDisasterLore(req)
}

// SyncDisasterLore triggers an immediate download/refresh of GSI Natural Disaster Monuments.
func (s *AppService) SyncDisasterLore() (int, error) {
	if s.collector == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.collector.SyncDisasterLore(ctx)
}

// GetHistorySyncStatus returns the current background history download/sync status per target.
func (s *AppService) GetHistorySyncStatus() []collector.HistorySyncStatus {
	if s.collector == nil {
		return nil
	}
	return s.collector.GetHistorySyncStatus()
}

// TriggerHistorySync triggers an immediate background history sync cycle.
func (s *AppService) TriggerHistorySync() {
	if s.collector != nil {
		s.collector.TriggerHistorySync()
	}
}

// ClearHistoryData deletes stored historical data for scope ("all" | "weather" | "marine" | "quake").
func (s *AppService) ClearHistoryData(scope string, resync bool) error {
	if s.collector != nil {
		return s.collector.ClearHistoryData(scope, resync)
	}
	return s.store.ClearHistoryData(scope)
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
		s.collector.TriggerHistorySync()
	}
	if s.app != nil {
		s.app.Event.Emit("twearth:foreground-resumed", time.Now().UTC().Format(time.RFC3339))
	}
}

// RefreshNow triggers an immediate data collection cycle.
func (s *AppService) RefreshNow() {
	if s.collector != nil {
		s.collector.TriggerRefresh()
		s.collector.TriggerHistorySync()
	}
}
