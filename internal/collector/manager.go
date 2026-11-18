package collector

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/notifier"
	"github.com/twsnmp/twearth/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Manager orchestrates all data collectors, persists snapshots/events, and emits Wails events.
type Manager struct {
	store     *store.Store
	notifier  *notifier.Dispatcher
	app       *application.App
	openMeteo *OpenMeteoClient
	liden     *LidenClient
	space     *SpaceClient
	volcano   *VolcanoClient
	quake     *QuakeClient

	mu          sync.RWMutex
	lastWeather *WeatherSnapshot
	lastMarine  *MarineSnapshot
	lastLiden   *LidenSnapshot
	lastSpace   *SpaceWeatherSnapshot

	refreshCh   chan struct{}
	reconnectCh chan struct{}
}

// NewManager creates a collector Manager wired to the persistent store.
func NewManager(st *store.Store) *Manager {
	return &Manager{
		store:       st,
		notifier:    notifier.NewDispatcher(),
		openMeteo:   NewOpenMeteoClient(),
		liden:       NewLidenClient(),
		space:       NewSpaceClient(),
		volcano:     NewVolcanoClient(),
		quake:       NewQuakeClient(),
		refreshCh:   make(chan struct{}, 1),
		reconnectCh: make(chan struct{}, 1),
	}
}

// SetApp attaches the Wails application for emitting real-time frontend events.
func (m *Manager) SetApp(app *application.App) {
	m.app = app
}

// Start launches background polling and P2PQuake WebSocket loops until ctx is canceled.
func (m *Manager) Start(ctx context.Context) {
	go m.quake.ListenP2PWebSocket(ctx, m.reconnectCh, func(ev QuakeEvent) {
		m.handleQuakeEvent(ev)
	})

	go m.pollLoop(ctx)
}

// TriggerRefresh requests an immediate poll & WebSocket reconnect (used on mobile foreground resume).
func (m *Manager) TriggerRefresh() {
	select {
	case m.refreshCh <- struct{}{}:
	default:
	}
	select {
	case m.reconnectCh <- struct{}{}:
	default:
	}
}

func (m *Manager) pollLoop(ctx context.Context) {
	m.runAllOnce(ctx)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.refreshCh:
			m.runAllOnce(ctx)
		case <-ticker.C:
			m.runAllOnce(ctx)
		}
	}
}

func (m *Manager) runAllOnce(ctx context.Context) {
	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}

	if w, err := m.openMeteo.FetchLocalWeather(ctx, cfg.Latitude, cfg.Longitude); err == nil {
		m.mu.Lock()
		m.lastWeather = &w
		m.mu.Unlock()
		m.emit("twearth:weather-updated", w)
	} else {
		log.Printf("collector openmeteo weather error: %v", err)
	}

	if grid, err := m.openMeteo.FetchJetStreamGrid(ctx); err == nil {
		m.emit("twearth:windgrid-updated", grid)
	} else {
		log.Printf("collector openmeteo windgrid error: %v", err)
	}

	if mar, err := m.openMeteo.FetchMarine(ctx, cfg.Latitude, cfg.Longitude); err == nil {
		m.mu.Lock()
		m.lastMarine = &mar
		m.mu.Unlock()
		m.emit("twearth:marine-updated", mar)
	} else {
		log.Printf("collector openmeteo marine error: %v", err)
	}

	if lid, err := m.liden.FetchLiden(ctx, cfg.Latitude, cfg.Longitude, cfg.LightningRadiusKm); err == nil {
		m.mu.Lock()
		m.lastLiden = &lid
		m.mu.Unlock()
		m.emit("twearth:liden-updated", lid)
		if lid.NearbyCount > 0 {
			m.recordAndNotify(ctx, cfg, store.EventRecord{
				ID:        "liden-" + lid.ValidTime,
				Type:      "TWEARTH-LIGHTNING",
				Severity:  "warning",
				Title:     fmt.Sprintf("自宅 %.0fkm 圏内で落雷検知 (%d件)", cfg.LightningRadiusKm, lid.NearbyCount),
				Detail:    fmt.Sprintf("観測時刻: %s", lid.ValidTime),
				Latitude:  cfg.Latitude,
				Longitude: cfg.Longitude,
				Timestamp: time.Now().UTC(),
			})
		}
	} else {
		log.Printf("collector liden error: %v", err)
	}

	if sp, err := m.space.FetchSpaceWeather(ctx); err == nil {
		m.mu.Lock()
		m.lastSpace = &sp
		m.mu.Unlock()
		m.emit("twearth:space-updated", sp)
		if sp.KpIndex >= cfg.KpIndexThreshold || sp.FlareClass == "M" || sp.FlareClass == "X" {
			m.recordAndNotify(ctx, cfg, store.EventRecord{
				ID:        fmt.Sprintf("space-%d", sp.Timestamp.Unix()/900),
				Type:      "TWEARTH-SPACE",
				Severity:  "warning",
				Title:     fmt.Sprintf("宇宙天気アラート Kp=%.1f / フレア=%s", sp.KpIndex, sp.FlareClass),
				Detail:    "NOAA SWPC",
				Timestamp: sp.Timestamp,
			})
		}
	} else {
		log.Printf("collector space error: %v", err)
	}

	if vols, err := m.volcano.FetchVolcanoes(ctx); err == nil {
		m.emit("twearth:volcano-updated", vols)
	} else {
		log.Printf("collector volcano error: %v", err)
	}

	if hist, err := m.quake.FetchP2PHistory(ctx); err == nil && len(hist) > 0 {
		m.emit("twearth:quake-history", hist)
	}

	if usgs, err := m.quake.FetchUSGS(ctx); err == nil && len(usgs) > 0 {
		m.emit("twearth:quake-usgs", usgs)
	}

	m.persistTimeSeriesSnapshot()
}

func (m *Manager) handleQuakeEvent(ev QuakeEvent) {
	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}
	rec := store.EventRecord{
		ID:        ev.ID,
		Type:      "TWEARTH-QUAKE",
		Severity:  "info",
		Title:     ev.Title,
		Detail:    ev.Hypocenter,
		Latitude:  ev.Latitude,
		Longitude: ev.Longitude,
		Timestamp: ev.OccurredAt,
	}
	if notifier.IntensityRank(ev.MaxScale) >= notifier.IntensityRank(cfg.QuakeMinIntensity) || ev.Magnitude >= 5.0 {
		rec.Severity = "warning"
		m.recordAndNotify(context.Background(), cfg, rec)
	} else {
		_ = m.store.SaveEvent(rec)
	}
	m.emit("twearth:quake-event", ev)
}

func (m *Manager) recordAndNotify(ctx context.Context, cfg store.Config, rec store.EventRecord) {
	_ = m.store.SaveEvent(rec)
	m.emit("twearth:alert-event", rec)
	if m.notifier != nil {
		for _, err := range m.notifier.Send(ctx, cfg, rec) {
			log.Printf("notifier error: %v", err)
		}
	}
}

func (m *Manager) persistTimeSeriesSnapshot() {
	m.mu.RLock()
	w := m.lastWeather
	mar := m.lastMarine
	lid := m.lastLiden
	sp := m.lastSpace
	m.mu.RUnlock()

	if w == nil && mar == nil && lid == nil && sp == nil {
		return
	}

	pt := store.TimeSeriesPoint{
		Timestamp: time.Now().UTC().Format("200601021504"),
	}
	if w != nil {
		pt.PressureMSL = w.PressureMSL
		pt.Temperature2m = w.Temperature2m
		pt.CAPE = w.CAPE
	}
	if mar != nil {
		pt.SeaSurfaceTemp = mar.SeaSurfaceTemperature
	}
	if lid != nil {
		pt.LightningCount = lid.NearbyCount
	}
	if sp != nil {
		pt.KpIndex = sp.KpIndex
	}
	_ = m.store.SaveTimeSeries(pt)
}

func (m *Manager) emit(eventName string, data any) {
	if m.app != nil {
		m.app.Event.Emit(eventName, data)
	}
}
