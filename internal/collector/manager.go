package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// LatestState holds the most recent cached snapshots for immediate hydration of new windows.
type LatestState struct {
	Weathers  []WeatherSnapshot            `json:"weathers"`
	Marines   []MarineSnapshot             `json:"marines"`
	WindGrid  []WindGridPoint              `json:"windGrid"`
	Synoptic  *SynopticData                `json:"synoptic,omitempty"`
	Liden     *LidenSnapshot               `json:"liden,omitempty"`
	Radar     *RadarTileInfo               `json:"radar,omitempty"`
	Kikikuru  *KikikuruInfo                `json:"kikikuru,omitempty"`
	Space     *SpaceWeatherSnapshot        `json:"space,omitempty"`
	Volcanoes []VolcanoInfo                `json:"volcanoes"`
	Quakes    []QuakeEvent                 `json:"quakes"`
	Disasters []store.DisasterRecord       `json:"disasters"`
	Rivers    []store.RiverTimeSeriesPoint `json:"rivers"`
	LoreCount int                          `json:"loreCount"`
}

// Manager orchestrates all data collectors, persists snapshots/events, and emits Wails events.
type Manager struct {
	store          *store.Store
	app            *application.App
	limiter        *RateLimiter
	httpCache      *HTTPCache
	openMeteo      *OpenMeteoClient
	liden          *LidenClient
	radar          *RadarClient
	kikikuru       *KikikuruClient
	space          *SpaceClient
	volcano        *VolcanoClient
	quake          *QuakeClient
	jmaExtra       *JmaExtraClient
	mlitRiver      *MlitRiverClient
	globalDisaster *GlobalDisasterClient
	gsiLore        *GsiLoreClient

	mu            sync.RWMutex
	lastWeathers  []WeatherSnapshot
	lastMarines   []MarineSnapshot
	lastWindGrid  []WindGridPoint
	lastSynoptic  *SynopticData
	lastLiden     *LidenSnapshot
	lastRadar     *RadarTileInfo
	lastKikikuru  *KikikuruInfo
	lastSpace     *SpaceWeatherSnapshot
	lastVolcanoes []VolcanoInfo
	lastQuakes    []QuakeEvent
	lastDisasters []store.DisasterRecord
	lastRivers    []store.RiverTimeSeriesPoint
	lastLoreCount int

	lastRunWeather        time.Time
	lastRunGrid           time.Time
	lastRunMarine         time.Time
	lastRunLiden          time.Time
	lastRunSpace          time.Time
	lastRunVolcano        time.Time
	lastRunQuake          time.Time
	lastRunJmaExtra       time.Time
	lastRunKikikuru       time.Time
	lastRunRiver          time.Time
	lastRunGlobalDisaster time.Time
	lastRunLore           time.Time

	refreshCh     chan struct{}
	reconnectCh   chan struct{}
	historySyncCh chan struct{}

	syncStatusMu    sync.RWMutex
	syncStatuses    map[string]HistorySyncStatus
	historyCancelMu sync.Mutex
	historyCancel   context.CancelFunc
}

// NewManager creates a collector Manager wired to the persistent store.
func NewManager(st *store.Store) *Manager {
	rl := NewRateLimiter()
	hc := NewHTTPCache()
	m := &Manager{
		store:          st,
		limiter:        rl,
		httpCache:      hc,
		openMeteo:      NewOpenMeteoClient(rl),
		liden:          NewLidenClient(),
		radar:          NewRadarClient(),
		kikikuru:       NewKikikuruClient(rl, hc),
		space:          NewSpaceClient(),
		volcano:        NewVolcanoClient(),
		quake:          NewQuakeClient(),
		jmaExtra:       NewJmaExtraClient(rl, hc),
		mlitRiver:      NewMlitRiverClient(rl, hc),
		globalDisaster: NewGlobalDisasterClient(rl, hc),
		gsiLore:        NewGsiLoreClient(rl, hc),
		refreshCh:      make(chan struct{}, 1),
		reconnectCh:    make(chan struct{}, 1),
		historySyncCh:  make(chan struct{}, 1),
		syncStatuses:   make(map[string]HistorySyncStatus),
	}
	if st != nil {
		m.migrateAlertsIfNeeded()
		if recs, err := st.ListRecentQuakes(100); err == nil && len(recs) > 0 {
			quakes := make([]QuakeEvent, 0, len(recs))
			for _, r := range recs {
				quakes = append(quakes, QuakeEvent{
					ID:         r.ID,
					Source:     r.Source,
					Code:       r.Code,
					Title:      r.Title,
					Hypocenter: r.Hypocenter,
					Magnitude:  r.Magnitude,
					MaxScale:   r.MaxScale,
					Latitude:   r.Latitude,
					Longitude:  r.Longitude,
					DepthKm:    r.DepthKm,
					Tsunami:    r.Tsunami,
					OccurredAt: r.OccurredAt,
				})
			}
			m.lastQuakes = quakes
		}
		if dis, err := st.ListRecentDisasters(200, false); err == nil && len(dis) > 0 {
			m.lastDisasters = dis
		}
		if rivs, err := st.ListLatestRiverStations(); err == nil && len(rivs) > 0 {
			m.lastRivers = rivs
		}
		if cnt, err := st.CountDisasterLore(); err == nil {
			m.lastLoreCount = cnt
		}
	}
	return m
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
		Disasters: append([]store.DisasterRecord(nil), m.lastDisasters...),
		Rivers:    append([]store.RiverTimeSeriesPoint(nil), m.lastRivers...),
		LoreCount: m.lastLoreCount,
	}
	if m.lastSynoptic != nil {
		cp := *m.lastSynoptic
		st.Synoptic = &cp
	}
	if m.lastLiden != nil {
		cp := *m.lastLiden
		st.Liden = &cp
	}
	if m.lastRadar != nil {
		cp := *m.lastRadar
		st.Radar = &cp
	}
	if m.lastKikikuru != nil {
		cp := *m.lastKikikuru
		st.Kikikuru = &cp
	}
	if m.lastSpace != nil {
		cp := *m.lastSpace
		st.Space = &cp
	}
	return st
}

// Start launches background polling, history sync, and P2PQuake WebSocket loops until ctx is canceled.
func (m *Manager) Start(ctx context.Context) {
	go m.quake.ListenP2PWebSocket(ctx, m.reconnectCh, func(ev QuakeEvent) {
		m.handleQuakeEvent(ev)
	})

	go m.pollLoop(ctx)
	go m.runHistorySync(ctx)
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
					baseSev := classifyPressureSeverity(s.PressureDelta3h, cfg.PressureDropHpa3h)
					effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
						Source:    "openmeteo",
						Category:  "pressure",
						Latitude:  s.Latitude,
						Longitude: s.Longitude,
					}, cfg)
					m.recordEvent(store.EventRecord{
						ID:           fmt.Sprintf("pressure-%s-%d", s.LocationID, s.Timestamp.Unix()/3600),
						Type:         "TWEARTH-PRESSURE",
						Severity:     effSev,
						BaseSeverity: baseSev,
						LocationID:   s.LocationID,
						Title:        fmt.Sprintf("%s: Rapid pressure drop (%.1f hPa / 3h)", s.LocationName, s.PressureDelta3h),
						Detail:       fmt.Sprintf("MSL %.1f hPa", s.PressureMSL),
						TitleKey:     "event.pressureDrop.title",
						DetailKey:    "event.pressureDrop.detail",
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
				if cfg.SnowfallAlertCm > 0 && s.Snowfall >= cfg.SnowfallAlertCm {
					baseSev := classifyPrecipSeverity(s.Snowfall, cfg.SnowfallAlertCm)
					effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
						Source:    "openmeteo",
						Category:  "precip",
						Latitude:  s.Latitude,
						Longitude: s.Longitude,
					}, cfg)
					m.recordEvent(store.EventRecord{
						ID:           fmt.Sprintf("snow-%s-%d", s.LocationID, s.Timestamp.Unix()/3600),
						Type:         "TWEARTH-PRECIP",
						Severity:     effSev,
						BaseSeverity: baseSev,
						LocationID:   s.LocationID,
						Title:        fmt.Sprintf("%s: Heavy snowfall (%.1f cm)", s.LocationName, s.Snowfall),
						Detail:       fmt.Sprintf("Snow depth %.0f cm / Temp %.1f °C", s.SnowDepth*100, s.Temperature2m),
						TitleKey:     "event.snow.title",
						DetailKey:    "event.snow.detail",
						Params: map[string]string{
							"location": s.LocationName,
							"snowfall": fmt.Sprintf("%.1f", s.Snowfall),
							"depth":    fmt.Sprintf("%.0f", s.SnowDepth*100),
							"temp":     fmt.Sprintf("%.1f", s.Temperature2m),
						},
						Latitude:  s.Latitude,
						Longitude: s.Longitude,
						Timestamp: s.Timestamp,
					})
				} else if cfg.RainAlertMm > 0 && s.Precipitation >= cfg.RainAlertMm {
					baseSev := classifyPrecipSeverity(s.Precipitation, cfg.RainAlertMm)
					effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
						Source:    "openmeteo",
						Category:  "precip",
						Latitude:  s.Latitude,
						Longitude: s.Longitude,
					}, cfg)
					m.recordEvent(store.EventRecord{
						ID:           fmt.Sprintf("rain-%s-%d", s.LocationID, s.Timestamp.Unix()/3600),
						Type:         "TWEARTH-PRECIP",
						Severity:     effSev,
						BaseSeverity: baseSev,
						LocationID:   s.LocationID,
						Title:        fmt.Sprintf("%s: Heavy rain (%.1f mm)", s.LocationName, s.Precipitation),
						Detail:       fmt.Sprintf("Rain %.1f mm / Showers %.1f mm", s.Rain, s.Showers),
						TitleKey:     "event.rain.title",
						DetailKey:    "event.rain.detail",
						Params: map[string]string{
							"location": s.LocationName,
							"precip":   fmt.Sprintf("%.1f", s.Precipitation),
							"rain":     fmt.Sprintf("%.1f", s.Rain),
							"showers":  fmt.Sprintf("%.1f", s.Showers),
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
					baseSev := classifyLightningSeverity(lid.NearbyCount)
					effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
						Source:    "liden",
						Category:  "lightning",
						Latitude:  primary.Latitude,
						Longitude: primary.Longitude,
					}, cfg)
					m.recordEvent(store.EventRecord{
						ID:           "liden-" + lid.ValidTime,
						Type:         "TWEARTH-LIGHTNING",
						Severity:     effSev,
						BaseSeverity: baseSev,
						LocationID:   primary.ID,
						Title:        fmt.Sprintf("%s %.0fkm 圏内で落雷検知 (%d件)", primary.Name, cfg.LightningRadiusKm, lid.NearbyCount),
						Detail:       fmt.Sprintf("観測時刻: %s", lid.ValidTime),
						TitleKey:     "event.lightning.title",
						DetailKey:    "event.lightning.detail",
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
		if err := m.limiter.Wait(ctx, ProviderJMA); err == nil {
			if rad, err := m.radar.FetchRadar(ctx); err == nil && rad.TileURLTemplate != "" {
				m.mu.Lock()
				m.lastRadar = &rad
				m.mu.Unlock()
				m.emit("twearth:radar-updated", rad)
			} else if err != nil {
				log.Printf("collector radar error: %v", err)
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
					sev := classifySpaceSeverity(sp.KpIndex, sp.FlareClass)
					m.recordEvent(store.EventRecord{
						ID:           fmt.Sprintf("space-%d", sp.Timestamp.Unix()/3600),
						Type:         "TWEARTH-SPACE",
						Severity:     sev,
						BaseSeverity: sev,
						Title:        fmt.Sprintf("宇宙天気アラート Kp=%.1f / フレア=%s", sp.KpIndex, sp.FlareClass),
						Detail:       "NOAA SWPC",
						TitleKey:     "event.space.title",
						DetailKey:    "event.space.detail",
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
				m.processVolcanoAlerts(vols, cfg)
			} else {
				log.Printf("collector volcano error: %v", err)
			}
		}
	}

	if force || now.Sub(m.lastRunQuake) >= 10*time.Minute {
		m.lastRunQuake = now
		if hist, err := m.quake.FetchP2PHistory(ctx); err == nil && len(hist) > 0 {
			_ = m.saveQuakeEventsToStore(hist)
			m.mu.Lock()
			m.lastQuakes = mergeQuakes(m.lastQuakes, hist)
			m.mu.Unlock()
			m.emit("twearth:quake-history", hist)
		}
		if err := m.limiter.Wait(ctx, ProviderUSGS); err == nil {
			if usgs, err := m.quake.FetchUSGS(ctx); err == nil && len(usgs) > 0 {
				_ = m.saveQuakeEventsToStore(usgs)
				m.mu.Lock()
				m.lastQuakes = mergeQuakes(m.lastQuakes, usgs)
				m.mu.Unlock()
				m.emit("twearth:quake-usgs", usgs)
			}
		}
	}

	m.runDisasterCollectors(ctx, cfg, now, force)
	m.persistTimeSeriesSnapshot(cfg)
}

func (m *Manager) runDisasterCollectors(ctx context.Context, cfg store.Config, now time.Time, force bool) {
	// 1. JMA Extra XML Feed (warnings, landslide, flood, avalanche)
	if !cfg.DisableJmaExtra && (force || now.Sub(m.lastRunJmaExtra) >= time.Duration(cfg.JmaExtraIntervalMin)*time.Minute) {
		m.lastRunJmaExtra = now
		cur, hasCur, _ := m.store.GetFeedCursor("jma_extra")
		useLong := !hasCur || len(cur.SeenIDs) == 0
		recs, nextSeen, notMod, err := m.jmaExtra.FetchExtraFeed(ctx, useLong, cur.SeenIDs)
		if err != nil {
			log.Printf("collector jma_extra error: %v", err)
		} else if !notMod {
			_ = m.store.SaveFeedCursor(store.FeedCursor{
				Key:        "jma_extra",
				SeenIDs:    nextSeen,
				LastSyncAt: now.UTC(),
			})
			if len(recs) > 0 {
				m.persistAndNotifyDisasters(recs)
			}
		}
	}

	// 2. JMA Kikikuru (landslide, inundation, flood risk distribution tiles)
	if !cfg.DisableKikikuru && (force || now.Sub(m.lastRunKikikuru) >= time.Duration(cfg.KikikuruIntervalMin)*time.Minute) {
		m.lastRunKikikuru = now
		info, notMod, err := m.kikikuru.FetchKikikuru(ctx)
		if err != nil {
			log.Printf("collector kikikuru error: %v", err)
		} else if !notMod && info != nil {
			m.mu.Lock()
			m.lastKikikuru = info
			m.mu.Unlock()
			m.emit("twearth:kikikuru-updated", info)
		}
	}

	// 3. MLIT River Water Levels (filtered near configured locations)
	if !cfg.DisableRiver && (force || now.Sub(m.lastRunRiver) >= time.Duration(cfg.RiverIntervalMin)*time.Minute) {
		m.lastRunRiver = now
		pts, alerts, err := m.mlitRiver.FetchRiverLevels(ctx, cfg.Locations, cfg.RiverRadiusKm)
		if err != nil {
			log.Printf("collector mlit_river error: %v", err)
		} else {
			if len(pts) > 0 {
				if err := m.store.SaveRiverTimeSeriesBatch(pts); err != nil {
					log.Printf("store SaveRiverTimeSeriesBatch error: %v", err)
				}
				if latest, err := m.store.ListLatestRiverStations(); err == nil {
					m.mu.Lock()
					m.lastRivers = latest
					m.mu.Unlock()
					m.emit("twearth:rivers-updated", latest)
				}
			}
			if len(alerts) > 0 {
				m.persistAndNotifyDisasters(alerts)
			}
		}
	}

	// 4. Global Disasters (GDACS + NASA EONET)
	if !cfg.DisableGlobalDisaster && (force || now.Sub(m.lastRunGlobalDisaster) >= time.Duration(cfg.GlobalDisasterIntervalMin)*time.Minute) {
		m.lastRunGlobalDisaster = now
		var combined []store.DisasterRecord
		if gdacs, notMod, err := m.globalDisaster.FetchGDACS(ctx); err != nil {
			log.Printf("collector gdacs error: %v", err)
		} else if !notMod {
			combined = append(combined, gdacs...)
		}
		if eonet, notMod, err := m.globalDisaster.FetchEONET(ctx); err != nil {
			log.Printf("collector eonet error: %v", err)
		} else if !notMod {
			combined = append(combined, eonet...)
		}
		if len(combined) > 0 {
			m.persistAndNotifyDisasters(combined)
		}
	}

	// 5. GSI Natural Disaster Lore (sync once if empty or >30 days old)
	if !cfg.DisableDisasterLore && (m.lastRunLore.IsZero() || now.Sub(m.lastRunLore) >= 24*time.Hour) {
		m.lastRunLore = now
		cur, hasCur, _ := m.store.GetFeedCursor("gsi_lore")
		cnt, _ := m.store.CountDisasterLore()
		if cnt == 0 || !hasCur || now.Sub(cur.LastSyncAt) >= 30*24*time.Hour {
			go func() {
				syncCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				if _, err := m.SyncDisasterLore(syncCtx); err != nil {
					log.Printf("collector gsi_lore sync error: %v", err)
				}
			}()
		}
	}
}

// SyncDisasterLore downloads GSI Natural Disaster Monuments and persists them locally.
func (m *Manager) SyncDisasterLore(ctx context.Context) (int, error) {
	items, notMod, err := m.gsiLore.FetchAllLore(ctx)
	if err != nil {
		return m.lastLoreCount, err
	}
	if !notMod && len(items) > 0 {
		if err := m.store.SaveDisasterLoreBatch(items); err != nil {
			return m.lastLoreCount, err
		}
	}
	_ = m.store.SaveFeedCursor(store.FeedCursor{
		Key:        "gsi_lore",
		LastSyncAt: time.Now().UTC(),
	})
	cnt, err := m.store.CountDisasterLore()
	if err == nil {
		m.mu.Lock()
		m.lastLoreCount = cnt
		m.mu.Unlock()
		m.emit("twearth:lore-updated", cnt)
	}
	return cnt, err
}

func (m *Manager) persistAndNotifyDisasters(items []store.DisasterRecord) {
	if len(items) == 0 {
		return
	}
	if err := m.store.SaveDisasterBatch(items); err != nil {
		log.Printf("store SaveDisasterBatch error: %v", err)
	}
	if recent, err := m.store.ListRecentDisasters(200, false); err == nil {
		m.mu.Lock()
		m.lastDisasters = recent
		m.mu.Unlock()
		m.emit("twearth:disasters-updated", recent)
	}
	// Record high-severity domestic alerts into the general event feed after distance attenuation.
	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}
	for _, d := range items {
		if d.Source == "jma_extra" || d.Source == "mlit_river" {
			effSev, _, _ := AdjustSeverityByDistance(d.Severity, AlertSeverityMeta{
				Source:    d.Source,
				Category:  "disaster",
				Latitude:  d.Latitude,
				Longitude: d.Longitude,
			}, cfg)
			if effSev == "critical" || effSev == "warning" {
				m.recordEvent(store.EventRecord{
					ID:           "dis-" + d.Source + "-" + d.ID,
					Type:         "TWEARTH-DISASTER",
					Severity:     effSev,
					BaseSeverity: d.Severity,
					Title:        d.Title,
					Detail:       d.Detail,
					Latitude:     d.Latitude,
					Longitude:    d.Longitude,
					Timestamp:    d.UpdatedAt,
				})
			}
		}
	}
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
		switch ev.Tsunami {
		case "Cancelled":
			titleKey = "event.tsunamiCancelled.title"
		case "MajorWarning":
			titleKey = "event.tsunamiMajor.title"
		case "Warning":
			titleKey = "event.tsunamiWarning.title"
		case "Watch":
			titleKey = "event.tsunamiWatch.title"
		default:
			titleKey = "event.tsunami.title"
		}
	} else if ev.Code == 556 {
		titleKey = "event.eew.title"
	}
	baseSev := classifyQuakeSeverity(ev, cfg.QuakeMinIntensity)
	effSev, _, _ := AdjustQuakeSeverityByDistance(baseSev, ev, cfg)
	rec := store.EventRecord{
		ID:           ev.ID,
		Type:         "TWEARTH-QUAKE",
		Severity:     effSev,
		BaseSeverity: baseSev,
		Title:        ev.Title,
		Detail:       ev.Hypocenter,
		TitleKey:     titleKey,
		DetailKey:    "event.quake.detail",
		Params: map[string]string{
			"hypocenter": ev.Hypocenter,
			"magnitude":  fmt.Sprintf("%.1f", ev.Magnitude),
			"maxScale":   ev.MaxScale,
			"depth":      fmt.Sprintf("%.0f", ev.DepthKm),
			"source":     ev.Source,
			"code":       strconv.Itoa(ev.Code),
			"tsunami":    ev.Tsunami,
		},
		Latitude:  ev.Latitude,
		Longitude: ev.Longitude,
		Timestamp: ev.OccurredAt,
	}
	if !(ev.Code == 552 && ev.Tsunami == "Cancelled") {
		if effSev != "info" {
			m.recordEvent(rec)
		} else {
			_ = m.store.SaveEvent(rec)
		}
	}
	_ = m.saveQuakeEventsToStore([]QuakeEvent{ev})
	m.mu.Lock()
	m.lastQuakes = mergeQuakes(m.lastQuakes, []QuakeEvent{ev})
	m.mu.Unlock()
	m.emit("twearth:quake-event", ev)
}

func (m *Manager) processVolcanoAlerts(vols []VolcanoInfo, cfg store.Config) {
	if len(vols) == 0 || m.store == nil {
		return
	}
	prevMap := make(map[string]int)
	if rawPrev, ok, _ := m.store.GetMetaValue("volcano_levels"); ok && rawPrev != "" {
		_ = json.Unmarshal([]byte(rawPrev), &prevMap)
	}
	nextMap := make(map[string]int, len(vols))
	for _, v := range vols {
		nextMap[v.Code] = v.AlertLevel
		if prevLvl, hadPrev := prevMap[v.Code]; hadPrev && v.AlertLevel > prevLvl && v.AlertLevel >= 2 {
			baseSev := classifyVolcanoSeverity(v.AlertLevel)
			effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
				Source:    "jma_volcano",
				Category:  "volcano",
				Latitude:  v.Latitude,
				Longitude: v.Longitude,
			}, cfg)
			m.recordEvent(store.EventRecord{
				ID:           fmt.Sprintf("volcano-%s-%d-%s", v.Code, v.AlertLevel, time.Now().UTC().Format("20060102")),
				Type:         "TWEARTH-VOLCANO",
				Severity:     effSev,
				BaseSeverity: baseSev,
				Title:        fmt.Sprintf("%s: 噴火警戒レベル %d (%s)", v.Name, v.AlertLevel, v.WarningTitle),
				Detail:       v.WarningTitle,
				Latitude:     v.Latitude,
				Longitude:    v.Longitude,
				Timestamp:    time.Now().UTC(),
			})
		}
	}
	if encoded, err := json.Marshal(nextMap); err == nil {
		_ = m.store.SetMetaValue("volcano_levels", string(encoded))
	}
}

func (m *Manager) recordEvent(rec store.EventRecord) {
	_ = m.store.SaveEvent(rec)
	m.emit("twearth:alert-event", rec)
}

func nearestCoastalSST(lat, lon float64, marines []MarineSnapshot, marineLocs []store.MarineLocation) float64 {
	if len(marines) == 0 {
		return 0
	}
	catByID := make(map[string]string, len(marineLocs))
	for _, ml := range marineLocs {
		catByID[ml.ID] = ml.Category
	}
	bestDist := -1.0
	bestSST := marines[0].SeaSurfaceTemperature
	// Prefer non-climate (nearshore/regional) marine locations first.
	for _, passClimate := range []bool{false, true} {
		for _, m := range marines {
			isClimate := catByID[m.LocationID] == "climate"
			if isClimate != passClimate {
				continue
			}
			d := HaversineKm(lat, lon, m.Latitude, m.Longitude)
			if bestDist < 0 || d < bestDist {
				bestDist = d
				bestSST = m.SeaSurfaceTemperature
			}
		}
		if bestDist >= 0 {
			return bestSST
		}
	}
	return bestSST
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
	kp := 0.0
	if sp != nil {
		kp = sp.KpIndex
	}
	lidenCount := 0
	if lid != nil {
		lidenCount = lid.NearbyCount
	}

	for _, w := range weathers {
		sst := nearestCoastalSST(w.Latitude, w.Longitude, marines, cfg.MarineLocations)
		pt := store.TimeSeriesPoint{
			LocationID:     w.LocationID,
			Timestamp:      ts,
			PressureMSL:    w.PressureMSL,
			Temperature2m:  w.Temperature2m,
			Precipitation:  w.Precipitation,
			Rain:           w.Rain,
			Snowfall:       w.Snowfall,
			SnowDepth:      w.SnowDepth,
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

	if len(marines) > 0 {
		mPts := make([]store.MarineTimeSeriesPoint, 0, len(marines))
		for _, mar := range marines {
			mPts = append(mPts, store.MarineTimeSeriesPoint{
				LocationID:            mar.LocationID,
				Timestamp:             ts,
				SeaSurfaceTemperature: mar.SeaSurfaceTemperature,
				WaveHeight:            mar.WaveHeight,
				OceanCurrentVelocity:  mar.OceanCurrentVelocity,
				OceanCurrentDirection: mar.OceanCurrentDirection,
				MissingSST:            mar.MissingSST,
				MissingWave:           mar.MissingWave,
				MissingCurrent:        mar.MissingCurrent,
			})
		}
		_ = m.store.SaveMarineTimeSeriesBatch(mPts)
	}
}

func (m *Manager) emit(eventName string, data any) {
	if m.app != nil {
		m.app.Event.Emit(eventName, data)
	}
}
