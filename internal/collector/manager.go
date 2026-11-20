package collector

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// LatestState holds the most recent cached snapshots for immediate hydration of new windows.
type LatestState struct {
	Weathers  []WeatherSnapshot     `json:"weathers"`
	Marines   []MarineSnapshot      `json:"marines"`
	WindGrid  []WindGridPoint       `json:"windGrid"`
	Synoptic  *SynopticData         `json:"synoptic,omitempty"`
	Liden     *LidenSnapshot        `json:"liden,omitempty"`
	Space     *SpaceWeatherSnapshot `json:"space,omitempty"`
	Volcanoes []VolcanoInfo         `json:"volcanoes"`
	Quakes    []QuakeEvent          `json:"quakes"`
}

// Manager orchestrates all data collectors, persists snapshots/events, and emits Wails events.
type Manager struct {
	store     *store.Store
	app       *application.App
	limiter   *RateLimiter
	openMeteo *OpenMeteoClient
	liden     *LidenClient
	space     *SpaceClient
	volcano   *VolcanoClient
	quake     *QuakeClient

	mu            sync.RWMutex
	lastWeathers  []WeatherSnapshot
	lastMarines   []MarineSnapshot
	lastWindGrid  []WindGridPoint
	lastSynoptic  *SynopticData
	lastLiden     *LidenSnapshot
	lastSpace     *SpaceWeatherSnapshot
	lastVolcanoes []VolcanoInfo
	lastQuakes    []QuakeEvent

	lastRunWeather time.Time
	lastRunGrid    time.Time
	lastRunMarine  time.Time
	lastRunLiden   time.Time
	lastRunSpace   time.Time
	lastRunVolcano time.Time
	lastRunQuake   time.Time

	refreshCh   chan struct{}
	reconnectCh chan struct{}
}

// NewManager creates a collector Manager wired to the persistent store.
func NewManager(st *store.Store) *Manager {
	rl := NewRateLimiter()
	return &Manager{
		store:       st,
		limiter:     rl,
		openMeteo:   NewOpenMeteoClient(rl),
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

// SearchCities delegates city geocoding lookup to OpenMeteoClient.
func (m *Manager) SearchCities(ctx context.Context, query, lang string) ([]CitySearchResult, error) {
	return m.openMeteo.SearchCities(ctx, query, lang)
}

// GetLatestState returns a copy of all cached snapshots for immediate frontend/window hydration.
func (m *Manager) GetLatestState() LatestState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st := LatestState{
		Weathers:  append([]WeatherSnapshot(nil), m.lastWeathers...),
		Marines:   append([]MarineSnapshot(nil), m.lastMarines...),
		WindGrid:  append([]WindGridPoint(nil), m.lastWindGrid...),
		Volcanoes: append([]VolcanoInfo(nil), m.lastVolcanoes...),
		Quakes:    append([]QuakeEvent(nil), m.lastQuakes...),
	}
	if m.lastSynoptic != nil {
		cp := *m.lastSynoptic
		st.Synoptic = &cp
	}
	if m.lastLiden != nil {
		cp := *m.lastLiden
		st.Liden = &cp
	}
	if m.lastSpace != nil {
		cp := *m.lastSpace
		st.Space = &cp
	}
	return st
}

// Start launches background polling and P2PQuake WebSocket loops until ctx is canceled.
func (m *Manager) Start(ctx context.Context) {
	go m.quake.ListenP2PWebSocket(ctx, m.reconnectCh, func(ev QuakeEvent) {
		m.handleQuakeEvent(ev)
	})

	go m.pollLoop(ctx)
}

// TriggerRefresh requests an immediate poll & WebSocket reconnect.
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
	m.runDue(ctx, true)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.refreshCh:
			m.runDue(ctx, true)
		case <-ticker.C:
			m.runDue(ctx, false)
		}
	}
}

func (m *Manager) runDue(ctx context.Context, force bool) {
	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}
	now := time.Now()
	primary := cfg.PrimaryLocation()

	if force || now.Sub(m.lastRunWeather) >= time.Duration(cfg.WeatherIntervalMin)*time.Minute {
		m.lastRunWeather = now
		if snaps, err := m.openMeteo.FetchMultiWeather(ctx, cfg.Locations); err == nil {
			m.mu.Lock()
			m.lastWeathers = snaps
			m.mu.Unlock()
			m.emit("twearth:weathers-updated", snaps)
			for _, s := range snaps {
				if s.LocationID == primary.ID {
					m.emit("twearth:weather-updated", s)
				}
				if s.PressureDelta3h <= -cfg.PressureDropHpa3h {
					m.recordEvent(store.EventRecord{
						ID:         fmt.Sprintf("pressure-%s-%d", s.LocationID, s.Timestamp.Unix()/3600),
						Type:       "TWEARTH-PRESSURE",
						Severity:   "warning",
						LocationID: s.LocationID,
						Title:      fmt.Sprintf("%s: Rapid pressure drop (%.1f hPa / 3h)", s.LocationName, s.PressureDelta3h),
						Detail:     fmt.Sprintf("MSL %.1f hPa", s.PressureMSL),
						TitleKey:   "event.pressureDrop.title",
						DetailKey:  "event.pressureDrop.detail",
						Params: map[string]string{
							"location": s.LocationName,
							"delta":    fmt.Sprintf("%.1f", s.PressureDelta3h),
							"pressure": fmt.Sprintf("%.1f", s.PressureMSL),
						},
						Latitude:  s.Latitude,
						Longitude: s.Longitude,
						Timestamp: s.Timestamp,
					})
				}
			}
		} else {
			log.Printf("collector openmeteo weather error: %v", err)
		}
	}

	if force || now.Sub(m.lastRunGrid) >= time.Duration(cfg.WindGridIntervalMin)*time.Minute {
		m.lastRunGrid = now
		if grid, err := m.openMeteo.FetchJetStreamGrid(ctx); err == nil {
			syn := BuildSynopticData(grid)
			m.mu.Lock()
			m.lastWindGrid = grid
			m.lastSynoptic = &syn
			m.mu.Unlock()
			m.emit("twearth:windgrid-updated", grid)
			m.emit("twearth:synoptic-updated", syn)
		} else {
			log.Printf("collector openmeteo windgrid error: %v", err)
		}
	}

	if force || now.Sub(m.lastRunMarine) >= time.Duration(cfg.MarineIntervalMin)*time.Minute {
		m.lastRunMarine = now
		if mars, err := m.openMeteo.FetchMultiMarine(ctx, cfg.MarineLocations); err == nil {
			m.mu.Lock()
			m.lastMarines = mars
			m.mu.Unlock()
			m.emit("twearth:marines-updated", mars)
			primaryMarine := cfg.PrimaryMarineLocation()
			for _, mar := range mars {
				if mar.LocationID == primaryMarine.ID {
					m.emit("twearth:marine-updated", mar)
					break
				}
			}
		} else {
			log.Printf("collector openmeteo marine error: %v", err)
		}
	}

	if force || now.Sub(m.lastRunLiden) >= time.Duration(cfg.LidenIntervalMin)*time.Minute {
		m.lastRunLiden = now
		if err := m.limiter.Wait(ctx, ProviderJMA); err == nil {
			if lid, err := m.liden.FetchLiden(ctx, primary.Latitude, primary.Longitude, cfg.LightningRadiusKm); err == nil {
				m.mu.Lock()
				m.lastLiden = &lid
				m.mu.Unlock()
				m.emit("twearth:liden-updated", lid)
				if lid.NearbyCount > 0 {
					m.recordEvent(store.EventRecord{
						ID:         "liden-" + lid.ValidTime,
						Type:       "TWEARTH-LIGHTNING",
						Severity:   "warning",
						LocationID: primary.ID,
						Title:      fmt.Sprintf("%s %.0fkm 圏内で落雷検知 (%d件)", primary.Name, cfg.LightningRadiusKm, lid.NearbyCount),
						Detail:     fmt.Sprintf("観測時刻: %s", lid.ValidTime),
						TitleKey:   "event.lightning.title",
						DetailKey:  "event.lightning.detail",
						Params: map[string]string{
							"location": primary.Name,
							"radius":   fmt.Sprintf("%.0f", cfg.LightningRadiusKm),
							"count":    fmt.Sprintf("%d", lid.NearbyCount),
							"time":     lid.ValidTime,
						},
						Latitude:  primary.Latitude,
						Longitude: primary.Longitude,
						Timestamp: time.Now().UTC(),
					})
				}
			} else {
				log.Printf("collector liden error: %v", err)
			}
		}
	}

	if force || now.Sub(m.lastRunSpace) >= time.Duration(cfg.SpaceIntervalMin)*time.Minute {
		m.lastRunSpace = now
		if err := m.limiter.Wait(ctx, ProviderNOAA); err == nil {
			if sp, err := m.space.FetchSpaceWeather(ctx); err == nil {
				m.mu.Lock()
				m.lastSpace = &sp
				m.mu.Unlock()
				m.emit("twearth:space-updated", sp)
				if sp.KpIndex >= cfg.KpIndexThreshold || sp.FlareClass == "M" || sp.FlareClass == "X" {
					m.recordEvent(store.EventRecord{
						ID:        fmt.Sprintf("space-%d", sp.Timestamp.Unix()/900),
						Type:      "TWEARTH-SPACE",
						Severity:  "warning",
						Title:     fmt.Sprintf("宇宙天気アラート Kp=%.1f / フレア=%s", sp.KpIndex, sp.FlareClass),
						Detail:    "NOAA SWPC",
						TitleKey:  "event.space.title",
						DetailKey: "event.space.detail",
						Params: map[string]string{
							"kp":    fmt.Sprintf("%.1f", sp.KpIndex),
							"flare": sp.FlareClass,
						},
						Timestamp: sp.Timestamp,
					})
				}
			} else {
				log.Printf("collector space error: %v", err)
			}
		}
	}

	if force || now.Sub(m.lastRunVolcano) >= time.Duration(cfg.VolcanoIntervalMin)*time.Minute {
		m.lastRunVolcano = now
		if err := m.limiter.Wait(ctx, ProviderJMA); err == nil {
			if vols, err := m.volcano.FetchVolcanoes(ctx); err == nil {
				m.mu.Lock()
				m.lastVolcanoes = vols
				m.mu.Unlock()
				m.emit("twearth:volcano-updated", vols)
			} else {
				log.Printf("collector volcano error: %v", err)
			}
		}
	}

	if force || now.Sub(m.lastRunQuake) >= 10*time.Minute {
		m.lastRunQuake = now
		if hist, err := m.quake.FetchP2PHistory(ctx); err == nil && len(hist) > 0 {
			m.mu.Lock()
			m.lastQuakes = mergeQuakes(m.lastQuakes, hist)
			m.mu.Unlock()
			m.emit("twearth:quake-history", hist)
		}
		if err := m.limiter.Wait(ctx, ProviderUSGS); err == nil {
			if usgs, err := m.quake.FetchUSGS(ctx); err == nil && len(usgs) > 0 {
				m.mu.Lock()
				m.lastQuakes = mergeQuakes(m.lastQuakes, usgs)
				m.mu.Unlock()
				m.emit("twearth:quake-usgs", usgs)
			}
		}
	}

	m.persistTimeSeriesSnapshot(cfg)
}

func mergeQuakes(existing, incoming []QuakeEvent) []QuakeEvent {
	byID := make(map[string]QuakeEvent, len(existing)+len(incoming))
	for _, q := range existing {
		byID[q.ID] = q
	}
	for _, q := range incoming {
		byID[q.ID] = q
	}
	out := make([]QuakeEvent, 0, len(byID))
	for _, q := range byID {
		out = append(out, q)
	}
	// Keep latest 100
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].OccurredAt.After(out[i].OccurredAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

func (m *Manager) handleQuakeEvent(ev QuakeEvent) {
	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}
	titleKey := "event.quake.title"
	if ev.Code == 552 {
		titleKey = "event.tsunami.title"
	} else if ev.Code == 556 {
		titleKey = "event.eew.title"
	}
	rec := store.EventRecord{
		ID:        ev.ID,
		Type:      "TWEARTH-QUAKE",
		Severity:  "info",
		Title:     ev.Title,
		Detail:    ev.Hypocenter,
		TitleKey:  titleKey,
		DetailKey: "event.quake.detail",
		Params: map[string]string{
			"hypocenter": ev.Hypocenter,
			"magnitude":  fmt.Sprintf("%.1f", ev.Magnitude),
			"maxScale":   ev.MaxScale,
			"depth":      fmt.Sprintf("%.0f", ev.DepthKm),
		},
		Latitude:  ev.Latitude,
		Longitude: ev.Longitude,
		Timestamp: ev.OccurredAt,
	}
	if IntensityRank(ev.MaxScale) >= IntensityRank(cfg.QuakeMinIntensity) || ev.Magnitude >= 5.0 {
		rec.Severity = "warning"
		m.recordEvent(rec)
	} else {
		_ = m.store.SaveEvent(rec)
	}
	m.mu.Lock()
	m.lastQuakes = mergeQuakes(m.lastQuakes, []QuakeEvent{ev})
	m.mu.Unlock()
	m.emit("twearth:quake-event", ev)
}

func (m *Manager) recordEvent(rec store.EventRecord) {
	_ = m.store.SaveEvent(rec)
	m.emit("twearth:alert-event", rec)
}

func (m *Manager) persistTimeSeriesSnapshot(cfg store.Config) {
	m.mu.RLock()
	weathers := append([]WeatherSnapshot(nil), m.lastWeathers...)
	marines := append([]MarineSnapshot(nil), m.lastMarines...)
	lid := m.lastLiden
	sp := m.lastSpace
	m.mu.RUnlock()

	if len(weathers) == 0 && len(marines) == 0 && lid == nil && sp == nil {
		return
	}

	ts := time.Now().UTC().Format("200601021504")
	primary := cfg.PrimaryLocation()
	sst := 0.0
	if len(marines) > 0 {
		sst = marines[0].SeaSurfaceTemperature
	}
	kp := 0.0
	if sp != nil {
		kp = sp.KpIndex
	}
	lidenCount := 0
	if lid != nil {
		lidenCount = lid.NearbyCount
	}

	for _, w := range weathers {
		pt := store.TimeSeriesPoint{
			LocationID:     w.LocationID,
			Timestamp:      ts,
			PressureMSL:    w.PressureMSL,
			Temperature2m:  w.Temperature2m,
			CAPE:           w.CAPE,
			SeaSurfaceTemp: sst,
			KpIndex:        kp,
		}
		if w.LocationID == primary.ID {
			pt.LightningCount = lidenCount
			// Also save under legacy un-prefixed timestamp for backward compatibility.
			legacyPt := pt
			legacyPt.LocationID = ""
			_ = m.store.SaveTimeSeries(legacyPt)
		}
		_ = m.store.SaveTimeSeries(pt)
	}
}

func (m *Manager) emit(eventName string, data any) {
	if m.app != nil {
		m.app.Event.Emit(eventName, data)
	}
}
