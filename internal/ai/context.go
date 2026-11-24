package ai

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

// ViewExplainRequest describes the UI screen and current filter/mode to explain.
type ViewExplainRequest struct {
	View           string `json:"view"` // "charts" | "synoptic" | "marine" | "events" | "map"
	Mode           string `json:"mode,omitempty"`
	LocationID     string `json:"locationId,omitempty"`
	MarineID       string `json:"marineId,omitempty"`
	Metric         string `json:"metric,omitempty"`
	RangeKey       string `json:"rangeKey,omitempty"`
	Granularity    string `json:"granularity,omitempty"`
	QuakeRadiusKm  int    `json:"quakeRadiusKm,omitempty"`
	RiverStationID string `json:"riverStationId,omitempty"`
	Lang           string `json:"lang,omitempty"`
}

func isJa(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "ja")
}

func resolveLocation(cfg store.Config, locationID string) store.ObservationLocation {
	for _, l := range cfg.Locations {
		if l.ID == locationID {
			return l
		}
	}
	return cfg.PrimaryLocation()
}

func resolveMarine(cfg store.Config, marineID string) store.MarineLocation {
	for _, m := range cfg.MarineLocations {
		if m.ID == marineID {
			return m
		}
	}
	return cfg.PrimaryMarineLocation()
}

func findWeather(st collector.LatestState, locID string) *collector.WeatherSnapshot {
	for i := range st.Weathers {
		if st.Weathers[i].LocationID == locID {
			return &st.Weathers[i]
		}
	}
	return nil
}

func findNearestMarine(cfg store.Config, st collector.LatestState, lat, lon float64) (*store.MarineLocation, *collector.MarineSnapshot) {
	var bestLoc *store.MarineLocation
	bestDist := math.MaxFloat64
	for i := range cfg.MarineLocations {
		ml := &cfg.MarineLocations[i]
		d := collector.HaversineKm(lat, lon, ml.Latitude, ml.Longitude)
		if d < bestDist {
			bestDist = d
			bestLoc = ml
		}
	}
	if bestLoc == nil {
		return nil, nil
	}
	for i := range st.Marines {
		if st.Marines[i].LocationID == bestLoc.ID {
			return bestLoc, &st.Marines[i]
		}
	}
	return bestLoc, nil
}

// BuildLocationPrompt builds the system and user prompt for explaining a registered location's current state.
func BuildLocationPrompt(cfg store.Config, st collector.LatestState, db *store.Store, locationID, lang string) (system string, prompt string) {
	loc := resolveLocation(cfg, locationID)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Target Observation Location: %s (%s)\n", loc.Name, loc.NameEn))
	sb.WriteString(fmt.Sprintf("- Coordinates: %.4f, %.4f (Primary: %v)\n", loc.Latitude, loc.Longitude, loc.IsPrimary))
	sb.WriteString(fmt.Sprintf("- Current Time (UTC): %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	// 1. Current weather & air quality
	sb.WriteString("## 1. Current Weather & Air Quality\n")
	if w := findWeather(st, loc.ID); w != nil {
		sb.WriteString(fmt.Sprintf("- Sea-Level Pressure: %.1f hPa (3h Delta: %+.1f hPa)\n", w.PressureMSL, w.PressureDelta3h))
		sb.WriteString(fmt.Sprintf("- Surface Pressure: %.1f hPa\n", w.SurfacePressure))
		sb.WriteString(fmt.Sprintf("- Temperature: %.1f °C | Humidity: %.0f%%\n", w.Temperature2m, w.RelativeHumidity2m))
		sb.WriteString(fmt.Sprintf("- Wind (10m): %.1f m/s (Direction: %.0f deg)\n", w.WindSpeed10m, w.WindDirection10m))
		sb.WriteString(fmt.Sprintf("- Precipitation: %.1f mm (Rain: %.1f mm, Showers: %.1f mm)\n", w.Precipitation, w.Rain, w.Showers))
		if w.Snowfall > 0 || w.SnowDepth > 0 {
			sb.WriteString(fmt.Sprintf("- Snowfall: %.1f cm | Snow Depth: %.0f cm\n", w.Snowfall, w.SnowDepth*100))
		}
		sb.WriteString(fmt.Sprintf("- CAPE (Atmospheric Instability): %.0f J/kg\n", w.CAPE))
		sb.WriteString(fmt.Sprintf("- Air Quality: PM2.5 = %.1f µg/m³, PM10 = %.1f µg/m³, Dust = %.1f µg/m³, UV Index = %.1f\n", w.PM25, w.PM10, w.Dust, w.UVIndex))
		sb.WriteString(fmt.Sprintf("- Observed At: %s\n", w.Timestamp.Format(time.RFC3339)))
	} else {
		sb.WriteString("- No current weather snapshot available yet.\n")
	}

	// 2. Recent 24h/7d history stats if store is available
	if db != nil {
		now := time.Now().UTC()
		from24h := now.Add(-24 * time.Hour).Format("200601021504")
		toNow := now.Format("200601021504")
		if pRes, err := db.QueryHistory(store.HistoryQuery{
			Kind:       "weather",
			LocationID: loc.ID,
			FromTS:     from24h,
			ToTS:       toNow,
			Metric:     "pressureMsl",
		}); err == nil && pRes.Stats.Count > 0 {
			sb.WriteString(fmt.Sprintf("- 24h Pressure Stats: Mean %.1f hPa, Min %.1f hPa, Max %.1f hPa, Trend %+.2f hPa/day\n",
				pRes.Stats.Mean, pRes.Stats.Min, pRes.Stats.Max, pRes.Stats.TrendPerDay))
		}
		if tRes, err := db.QueryHistory(store.HistoryQuery{
			Kind:       "weather",
			LocationID: loc.ID,
			FromTS:     from24h,
			ToTS:       toNow,
			Metric:     "temperature2m",
		}); err == nil && tRes.Stats.Count > 0 {
			sb.WriteString(fmt.Sprintf("- 24h Temperature Stats: Mean %.1f °C, Min %.1f °C, Max %.1f °C\n",
				tRes.Stats.Mean, tRes.Stats.Min, tRes.Stats.Max))
		}
	}

	// 3. Nearest Marine SST
	if ml, ms := findNearestMarine(cfg, st, loc.Latitude, loc.Longitude); ml != nil && ms != nil {
		dist := collector.HaversineKm(loc.Latitude, loc.Longitude, ml.Latitude, ml.Longitude)
		sb.WriteString("\n## 2. Nearest Marine Conditions\n")
		sb.WriteString(fmt.Sprintf("- Sea Area: %s (%.0f km away) | SST: %.1f °C | Wave Height: %.2f m | Current: %.2f m/s\n",
			ml.Name, dist, ms.SeaSurfaceTemperature, ms.WaveHeight, ms.OceanCurrentVelocity))
	}

	// 4. Lightning & Space Weather
	sb.WriteString("\n## 3. Lightning & Space Weather\n")
	if st.Liden != nil {
		sb.WriteString(fmt.Sprintf("- LIDEN Lightning: %d nearby (within %.0f km of primary), %d total nationwide (Valid: %s)\n",
			st.Liden.NearbyCount, st.Liden.RadiusKm, st.Liden.TotalCount, st.Liden.ValidTime))
	}
	if st.Space != nil {
		sb.WriteString(fmt.Sprintf("- Space Weather (NOAA SWPC): Kp Index = %.1f, X-Ray Flare Class = %s (Flux: %.2e)\n",
			st.Space.KpIndex, st.Space.FlareClass, st.Space.XRayFlux))
	}

	// 5. Nearby Earthquakes (within 300km or top recent)
	sb.WriteString("\n## 4. Recent Earthquakes Near Location (within 300 km)\n")
	type quakeDist struct {
		q    collector.QuakeEvent
		dist float64
	}
	var nearQuakes []quakeDist
	for _, q := range st.Quakes {
		d := collector.HaversineKm(loc.Latitude, loc.Longitude, q.Latitude, q.Longitude)
		if d <= 300 {
			nearQuakes = append(nearQuakes, quakeDist{q: q, dist: d})
		}
	}
	sort.Slice(nearQuakes, func(i, j int) bool {
		return nearQuakes[i].q.OccurredAt.After(nearQuakes[j].q.OccurredAt)
	})
	if len(nearQuakes) == 0 {
		sb.WriteString("- No recent earthquakes within 300 km.\n")
	} else {
		for i, item := range nearQuakes {
			if i >= 8 {
				break
			}
			sb.WriteString(fmt.Sprintf("- %s | %s | M%.1f | MaxScale: %s | Depth: %.0f km | Distance: %.0f km\n",
				item.q.OccurredAt.Format(time.RFC3339), item.q.Hypocenter, item.q.Magnitude, item.q.MaxScale, item.q.DepthKm, item.dist))
		}
	}

	// 6. Nearby River Stations
	sb.WriteString("\n## 5. Nearby River Water / Rainfall Stations\n")
	type riverDist struct {
		r    store.RiverTimeSeriesPoint
		dist float64
	}
	var nearRivers []riverDist
	radiusKm := cfg.RiverRadiusKm
	if radiusKm <= 0 {
		radiusKm = 30
	}
	for _, r := range st.Rivers {
		d := collector.HaversineKm(loc.Latitude, loc.Longitude, r.Latitude, r.Longitude)
		if d <= radiusKm {
			nearRivers = append(nearRivers, riverDist{r: r, dist: d})
		}
	}
	sort.Slice(nearRivers, func(i, j int) bool { return nearRivers[i].dist < nearRivers[j].dist })
	if len(nearRivers) == 0 {
		sb.WriteString(fmt.Sprintf("- No river stations within %.0f km.\n", radiusKm))
	} else {
		for i, item := range nearRivers {
			if i >= 6 {
				break
			}
			sb.WriteString(fmt.Sprintf("- %s (%s) | %.1f km | Level: %.2f | Status: %s (Warn: %.1f, Danger: %.1f)\n",
				item.r.RiverName, item.r.StationName, item.dist, item.r.WaterLevelM, item.r.Status, item.r.WarningLevelM, item.r.DangerLevelM))
		}
	}

	// 7. Active Disasters / Warnings
	sb.WriteString("\n## 6. Active Disasters & Warnings\n")
	disCount := 0
	for _, d := range st.Disasters {
		if !d.IsActive {
			continue
		}
		dist := collector.HaversineKm(loc.Latitude, loc.Longitude, d.Latitude, d.Longitude)
		if d.Latitude == 0 && d.Longitude == 0 || dist <= 300 {
			sb.WriteString(fmt.Sprintf("- [%s/%s] %s (%s, %.0f km): %s\n",
				d.Source, d.Severity, d.Title, d.AreaName, dist, d.Detail))
			disCount++
			if disCount >= 8 {
				break
			}
		}
	}
	if disCount == 0 {
		sb.WriteString("- No active nearby disasters or warnings.\n")
	}

	// 8. Nearby Natural Disaster Lore
	if db != nil {
		if lore, err := db.QueryDisasterLore(store.DisasterLoreQuery{
			Latitude:  loc.Latitude,
			Longitude: loc.Longitude,
			RadiusKm:  20,
			Limit:     5,
		}); err == nil && len(lore) > 0 {
			sb.WriteString("\n## 7. Nearby Natural Disaster Monuments (within 20 km)\n")
			for _, l := range lore {
				sb.WriteString(fmt.Sprintf("- %s (%s, %s): %s\n", l.Name, l.DisasterKind, l.YearText, l.Summary))
			}
		}
	}

	if isJa(lang) {
		system = `あなたは気象・防災・自然環境監視の専門家です。
提示された登録地点の現在観測データ（気象・気圧変化・大気質・近隣海域・落雷・地震・河川・警報・災害伝承碑・宇宙天気）を総合的に分析し、以下の構成で分かりやすく日本語で解説してください：
1. **現在の環境・気象状態の要約**（体感、気圧変化の傾向、天候や大気質のポイント）
2. **注意すべきリスク・異常兆候**（急激な気圧低下、大気不安定・落雷、地震・河川・災害警報、健康/生活への影響など）
3. **推奨される備え・アクション**（外出・防災・生活上の具体的なアドバイス）`
	} else {
		system = `You are an expert in meteorology, disaster prevention, and environmental monitoring.
Analyze the provided real-time observation data for the registered location (weather, pressure trend, air quality, nearby marine SST, lightning, earthquakes, river levels, active disasters, disaster lore, and space weather) and explain clearly in English:
1. **Current Environmental & Weather Summary**
2. **Notable Risks, Anomalies & Warning Signs**
3. **Recommended Precautions & Actions**`
	}

	return system, sb.String()
}

// BuildViewPrompt builds the system and user prompt for explaining a specific screen/panel.
func BuildViewPrompt(cfg store.Config, st collector.LatestState, db *store.Store, req ViewExplainRequest) (system string, prompt string) {
	var sb strings.Builder
	loc := resolveLocation(cfg, req.LocationID)
	mloc := resolveMarine(cfg, req.MarineID)

	sb.WriteString(fmt.Sprintf("# Screen / View Explanation Request\n- View: %s\n- Mode: %s\n- Selected City: %s\n- Selected Marine: %s\n\n",
		req.View, req.Mode, loc.Name, mloc.Name))

	switch req.View {
	case "charts":
		switch req.Mode {
		case "compare":
			sb.WriteString("## Multi-City Current Comparison\n")
			for _, l := range cfg.Locations {
				if w := findWeather(st, l.ID); w != nil {
					sb.WriteString(fmt.Sprintf("- %s: Temp %.1f°C, Pressure %.1f hPa (3h %+.1f), Humidity %.0f%%, Precip %.1fmm, Snow %.1fcm, CAPE %.0f J/kg\n",
						l.Name, w.Temperature2m, w.PressureMSL, w.PressureDelta3h, w.RelativeHumidity2m, w.Precipitation, w.Snowfall, w.CAPE))
				}
			}
		case "trend":
			metric := req.Metric
			if metric == "" {
				metric = "temperature2m"
			}
			sb.WriteString(fmt.Sprintf("## Historical Trend (Location: %s, Metric: %s, Range: %s)\n", loc.Name, metric, req.RangeKey))
			if db != nil {
				now := time.Now().UTC()
				from := rangeStartKey(req.RangeKey, now)
				if res, err := db.QueryHistory(store.HistoryQuery{
					Kind:        "weather",
					LocationID:  loc.ID,
					FromTS:      from,
					ToTS:        now.Format("200601021504"),
					Metric:      metric,
					Granularity: req.Granularity,
				}); err == nil {
					sb.WriteString(fmt.Sprintf("- Count: %d | Mean: %.2f | Min: %.2f | Max: %.2f | StdDev: %.2f | Trend/day: %+.4f\n",
						res.Stats.Count, res.Stats.Mean, res.Stats.Min, res.Stats.Max, res.Stats.StdDev, res.Stats.TrendPerDay))
					n := len(res.Points)
					if n > 0 {
						sb.WriteString("- Recent sample points:\n")
						start := max(0, n-10)
						for _, pt := range res.Points[start:] {
							sb.WriteString(fmt.Sprintf("  - %s: %.2f\n", pt.Label, pt.Value))
						}
					}
				}
			}
		case "quake":
			sb.WriteString(fmt.Sprintf("## Earthquake History (Center: %s, Radius: %d km, Range: %s)\n", loc.Name, req.QuakeRadiusKm, req.RangeKey))
			if db != nil {
				now := time.Now().UTC()
				fromT := rangeStartTime(req.RangeKey, now, 365*24*time.Hour)
				if res, err := db.QueryQuakeHistory(store.QuakeHistoryQuery{
					Latitude:  loc.Latitude,
					Longitude: loc.Longitude,
					RadiusKm:  float64(req.QuakeRadiusKm),
					FromTime:  fromT,
					ToTime:    now,
				}); err == nil {
					sb.WriteString(fmt.Sprintf("- Total Count: %d | Max Magnitude: %.1f | Avg Magnitude: %.1f | Max Scale: %s\n",
						res.TotalCount, res.MaxMagnitude, res.AvgMagnitude, res.MaxScale))
				}
			}
		case "disaster":
			sb.WriteString(fmt.Sprintf("## Disaster History (Center: %s, Range: %s)\n", loc.Name, req.RangeKey))
			if db != nil {
				now := time.Now().UTC()
				fromT := rangeStartTime(req.RangeKey, now, 365*24*time.Hour)
				if res, err := db.QueryDisasterHistory(store.DisasterHistoryQuery{
					Latitude:  loc.Latitude,
					Longitude: loc.Longitude,
					RadiusKm:  100,
					FromTime:  fromT,
					ToTime:    now,
				}); err == nil {
					sb.WriteString(fmt.Sprintf("- Total: %d | Active: %d | Critical: %d | Warning: %d | Nearby Lore: %d\n",
						res.TotalCount, res.ActiveCount, res.CriticalCount, res.WarningCount, res.NearbyLore.TotalCount))
				}
			}
		case "river":
			sb.WriteString("## River Water Level Monitoring\n")
			for i, r := range st.Rivers {
				if i >= 12 {
					break
				}
				sb.WriteString(fmt.Sprintf("- %s (%s): %.2f m [Status: %s, Warn: %.1f, Danger: %.1f]\n",
					r.RiverName, r.StationName, r.WaterLevelM, r.Status, r.WarningLevelM, r.DangerLevelM))
			}
		}

	case "synoptic":
		sb.WriteString("## Synoptic Pressure & Upper-Air Analysis\n")
		if st.Synoptic != nil {
			sb.WriteString(fmt.Sprintf("- Isobars Count: %d | Pressure Centers: %d\n", len(st.Synoptic.Isobars), len(st.Synoptic.Centers)))
			for _, c := range st.Synoptic.Centers {
				sb.WriteString(fmt.Sprintf("- Center [%s]: %.1f hPa at (%.1f, %.1f)\n", c.Kind, c.Pressure, c.Latitude, c.Longitude))
			}
		}
		if len(st.WindGrid) > 0 {
			var max250, minP, maxP float64
			minP = st.WindGrid[0].PressureMSL
			maxP = st.WindGrid[0].PressureMSL
			for _, pt := range st.WindGrid {
				if pt.WindSpeed250hPa > max250 {
					max250 = pt.WindSpeed250hPa
				}
				if pt.PressureMSL < minP {
					minP = pt.PressureMSL
				}
				if pt.PressureMSL > maxP {
					maxP = pt.PressureMSL
				}
			}
			sb.WriteString(fmt.Sprintf("- Upper-Air Grid (%d pts): Max 250hPa Jet Stream = %.1f m/s, Surface Pressure Range = %.1f - %.1f hPa\n",
				len(st.WindGrid), max250, minP, maxP))
		}

	case "marine":
		sb.WriteString("## Marine & Sea Surface Temperature (SST) Monitor\n")
		for _, m := range st.Marines {
			sb.WriteString(fmt.Sprintf("- %s: SST %.1f°C | Wave %.2f m | Current %.2f m/s\n",
				m.LocationName, m.SeaSurfaceTemperature, m.WaveHeight, m.OceanCurrentVelocity))
		}
		if db != nil && mloc.ID != "" {
			now := time.Now().UTC()
			if res, err := db.QueryHistory(store.HistoryQuery{
				Kind:       "marine",
				LocationID: mloc.ID,
				FromTS:     now.Add(-30 * 24 * time.Hour).Format("200601021504"),
				ToTS:       now.Format("200601021504"),
				Metric:     "seaSurfaceTemperature",
			}); err == nil && res.Stats.Count > 0 {
				sb.WriteString(fmt.Sprintf("\n### Selected Marine Point 30d SST Trend (%s)\n- Mean: %.2f°C, Min: %.2f°C, Max: %.2f°C, Trend/day: %+.4f°C\n",
					mloc.Name, res.Stats.Mean, res.Stats.Min, res.Stats.Max, res.Stats.TrendPerDay))
			}
		}

	case "events":
		sb.WriteString("## Recent Alert Events & Disasters\n")
		if db != nil {
			if evs, err := db.ListEvents(25); err == nil {
				for _, ev := range evs {
					sb.WriteString(fmt.Sprintf("- [%s] (%s) %s: %s\n", ev.Severity, ev.Type, ev.Title, ev.Detail))
				}
			}
		}
		if len(st.Quakes) > 0 {
			sb.WriteString("\n### Latest Earthquakes\n")
			for i, q := range st.Quakes {
				if i >= 8 {
					break
				}
				sb.WriteString(fmt.Sprintf("- %s | %s M%.1f MaxScale %s\n", q.OccurredAt.Format(time.RFC3339), q.Hypocenter, q.Magnitude, q.MaxScale))
			}
		}

	default:
		// "map" or general overview
		return BuildLocationPrompt(cfg, st, db, req.LocationID, req.Lang)
	}

	if isJa(req.Lang) {
		system = `あなたは気象・海洋・地震防災データの解説専門家です。
ユーザーが現在表示している画面（チャート、気圧配置、海洋監視、イベント履歴など）のデータに基づき、
1. 表示されているデータの全体傾向・特徴
2. 読み取れる重要なポイントや異常・変化の兆候
3. 今後の見通しや注目すべき指標・注意点
を日本語で分かりやすく解説してください。`
	} else {
		system = `You are an expert in meteorology, oceanography, and disaster analytics.
Based on the data currently displayed on the user's screen (charts, synoptic pressure, marine SST, or event feed), clearly explain:
1. Overall trends and key characteristics in the displayed data
2. Notable anomalies, patterns, or warning signs
3. What to watch next and practical takeaways`
	}

	return system, sb.String()
}

// BuildEventPrompt builds the system and user prompt for explaining a specific event/alert.
func BuildEventPrompt(cfg store.Config, ev store.EventRecord, lang string) (system string, prompt string) {
	loc := cfg.PrimaryLocation()
	var sb strings.Builder
	sb.WriteString("# Target Alert / Natural Event\n")
	sb.WriteString(fmt.Sprintf("- ID: %s\n", ev.ID))
	sb.WriteString(fmt.Sprintf("- Type: %s\n", ev.Type))
	sb.WriteString(fmt.Sprintf("- Severity: %s\n", ev.Severity))
	sb.WriteString(fmt.Sprintf("- Title: %s\n", ev.Title))
	if ev.Detail != "" {
		sb.WriteString(fmt.Sprintf("- Detail: %s\n", ev.Detail))
	}
	sb.WriteString(fmt.Sprintf("- Timestamp: %s\n", ev.Timestamp.Format(time.RFC3339)))
	if ev.Latitude != 0 || ev.Longitude != 0 {
		dist := collector.HaversineKm(loc.Latitude, loc.Longitude, ev.Latitude, ev.Longitude)
		sb.WriteString(fmt.Sprintf("- Coordinates: %.4f, %.4f (Approx. %.1f km from primary location %s)\n",
			ev.Latitude, ev.Longitude, dist, loc.Name))
	}
	if len(ev.Params) > 0 {
		sb.WriteString("- Parameters:\n")
		for k, v := range ev.Params {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", k, v))
		}
	}

	if isJa(lang) {
		system = `あなたは自然災害・気象・宇宙天気アラートの解説専門家です。
提示されたイベントの詳細を分析し、
1. このイベントは何を意味しているか（発生原因や指標の意味）
2. 登録地点周辺への影響・リスク評価
3. 推奨される確認事項・防災アクション
を日本語で分かりやすく解説してください。`
	} else {
		system = `You are an expert in natural disaster, weather, and space weather alerts.
Analyze the provided event and clearly explain:
1. What this event means and its likely cause
2. Impact and risk assessment for the monitored location
3. Recommended checks and safety actions`
	}
	return system, sb.String()
}

// BuildQASystemPrompt builds a concise real-time system context for interactive Q&A.
func BuildQASystemPrompt(cfg store.Config, st collector.LatestState, locationID, lang string) string {
	loc := resolveLocation(cfg, locationID)
	var sb strings.Builder

	if isJa(lang) {
		sb.WriteString("あなたは twearth（地球環境・気象・防災監視アプリ）のAIアシスタントです。\n")
		sb.WriteString("以下のリアルタイム観測データを踏まえて、ユーザーの質問に正確かつ分かりやすく日本語で回答してください。推測やデータにない数値は捏造しないでください。\n\n")
	} else {
		sb.WriteString("You are the AI assistant for twearth (a real-time natural environment and disaster monitoring app).\n")
		sb.WriteString("Answer the user's questions accurately and clearly based on the current observation context below. Do not fabricate numbers not present in the context.\n\n")
	}

	sb.WriteString(fmt.Sprintf("## Primary/Selected City: %s (%.4f, %.4f)\n", loc.Name, loc.Latitude, loc.Longitude))
	for _, l := range cfg.Locations {
		if w := findWeather(st, l.ID); w != nil {
			sb.WriteString(fmt.Sprintf("- %s: %.1f°C, %.1f hPa (3h %+.1f), Humidity %.0f%%, Precip %.1fmm, Snow %.1fcm, CAPE %.0f, PM2.5 %.1f\n",
				l.Name, w.Temperature2m, w.PressureMSL, w.PressureDelta3h, w.RelativeHumidity2m, w.Precipitation, w.Snowfall, w.CAPE, w.PM25))
		}
	}
	if st.Space != nil {
		sb.WriteString(fmt.Sprintf("- Space Weather: Kp=%.1f, Flare=%s\n", st.Space.KpIndex, st.Space.FlareClass))
	}
	if st.Liden != nil {
		sb.WriteString(fmt.Sprintf("- Lightning (LIDEN): %d nearby / %d total\n", st.Liden.NearbyCount, st.Liden.TotalCount))
	}
	if len(st.Marines) > 0 {
		sb.WriteString("## Marine SST:\n")
		for i, m := range st.Marines {
			if i >= 6 {
				break
			}
			sb.WriteString(fmt.Sprintf("- %s: %.1f°C (Wave %.2fm)\n", m.LocationName, m.SeaSurfaceTemperature, m.WaveHeight))
		}
	}
	if len(st.Quakes) > 0 {
		sb.WriteString("## Recent Earthquakes:\n")
		for i, q := range st.Quakes {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("- %s: %s M%.1f MaxScale %s\n", q.OccurredAt.Format("01/02 15:04"), q.Hypocenter, q.Magnitude, q.MaxScale))
		}
	}
	activeDis := 0
	for _, d := range st.Disasters {
		if d.IsActive {
			if activeDis == 0 {
				sb.WriteString("## Active Disasters:\n")
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", d.Severity, d.Title, d.AreaName))
			activeDis++
			if activeDis >= 5 {
				break
			}
		}
	}
	return sb.String()
}

func rangeStartKey(rangeKey string, now time.Time) string {
	t := rangeStartTime(rangeKey, now, 30*24*time.Hour)
	if t.IsZero() {
		return ""
	}
	return t.Format("200601021504")
}

func rangeStartTime(rangeKey string, now time.Time, fallback time.Duration) time.Time {
	switch rangeKey {
	case "24h":
		return now.Add(-24 * time.Hour)
	case "7d":
		return now.Add(-7 * 24 * time.Hour)
	case "30d":
		return now.Add(-30 * 24 * time.Hour)
	case "90d":
		return now.Add(-90 * 24 * time.Hour)
	case "1y":
		return now.Add(-365 * 24 * time.Hour)
	case "3y":
		return now.Add(-3 * 365 * 24 * time.Hour)
	case "5y":
		return now.Add(-5 * 365 * 24 * time.Hour)
	case "10y":
		return now.Add(-10 * 365 * 24 * time.Hour)
	case "30y":
		return now.Add(-30 * 365 * 24 * time.Hour)
	case "all":
		return time.Time{}
	default:
		return now.Add(-fallback)
	}
}
