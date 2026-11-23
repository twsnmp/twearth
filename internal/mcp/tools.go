//go:build !android && !ios

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
)

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCallResult struct {
	Content []toolTextContent `json:"content"`
	IsError bool              `json:"isError,omitempty"`
}

type toolTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *desktopService) listToolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "get_earth_summary",
			Description: "Get a comprehensive real-time summary of all monitored cities' weather, marine SST, space weather, lightning, synoptic pressure centers, active disasters, and recent earthquakes.",
			InputSchema: objSchema(nil, nil),
		},
		{
			Name:        "get_current_weather",
			Description: "Get current atmospheric and air quality observations (pressure, 3h pressure delta, temperature, humidity, precipitation, snow, wind, CAPE, PM2.5, dust, UV) for registered observation cities.",
			InputSchema: objSchema(map[string]any{
				"location_id": strProp("Optional location ID to filter by a specific city. Omit to return all registered cities."),
			}, nil),
		},
		{
			Name:        "get_current_marine",
			Description: "Get current marine observations (sea surface temperature, wave height, ocean current velocity and direction) for registered marine points.",
			InputSchema: objSchema(map[string]any{
				"location_id": strProp("Optional marine location ID to filter by a specific point. Omit to return all registered marine points."),
			}, nil),
		},
		{
			Name:        "get_synoptic_and_space",
			Description: "Get synoptic surface pressure analysis (High/Low centers, isobar count), upper-air 250hPa jet stream / 500hPa summary, LIDEN lightning counts, and NOAA SWPC space weather (Kp index, X-ray flare class).",
			InputSchema: objSchema(map[string]any{
				"include_wind_grid": boolProp("If true, include all upper-air wind grid points (default false, returns summary stats only)."),
			}, nil),
		},
		{
			Name:        "list_alert_events",
			Description: "List detected natural/disaster alert events (earthquakes, rapid pressure drops, nearby lightning, heavy rain/snow, volcano warnings, domestic disasters, space weather).",
			InputSchema: objSchema(map[string]any{
				"hours":        intProp("Lookback window in hours (default 168 = 7 days; <=0 returns all stored events up to limit)."),
				"type":         strProp("Optional event type filter (e.g. TWEARTH-QUAKE, TWEARTH-PRESSURE, TWEARTH-LIGHTNING, TWEARTH-PRECIP, TWEARTH-VOLCANO, TWEARTH-DISASTER, TWEARTH-SPACE)."),
				"min_severity": strProp("Minimum severity filter: info, warning, or critical."),
				"limit":        intProp("Maximum number of events to return (default 100, max 500)."),
			}, nil),
		},
		{
			Name:        "list_recent_quakes",
			Description: "List recent earthquakes and tsunami notices from P2PQuake and USGS, with optional filtering by magnitude, intensity, or distance from coordinates.",
			InputSchema: objSchema(map[string]any{
				"min_magnitude": numProp("Optional minimum magnitude."),
				"min_intensity": strProp("Optional minimum JMA seismic intensity (e.g. 1, 2, 3, 4, 5-, 5+, 6-, 6+, 7)."),
				"latitude":      numProp("Optional center latitude for radius filtering."),
				"longitude":     numProp("Optional center longitude for radius filtering."),
				"radius_km":     numProp("Optional radius in km around latitude/longitude."),
				"limit":         intProp("Maximum number of earthquakes to return (default 50, max 200)."),
			}, nil),
		},
		{
			Name:        "list_volcanoes",
			Description: "List active volcanoes monitored by JMA with their current eruption alert levels and warning titles.",
			InputSchema: objSchema(map[string]any{
				"min_alert_level": intProp("Minimum eruption alert level to include (default 1; set 2+ to see elevated warnings only)."),
			}, nil),
		},
		{
			Name:        "list_active_disasters",
			Description: "List domestic and global disaster records (JMA Extra XML warnings, MLIT river flood alerts, GDACS, NASA EONET).",
			InputSchema: objSchema(map[string]any{
				"active_only": boolProp("If true (default), return only currently active disasters."),
				"category":    strProp("Optional category filter (flood, landslide, snow, wildfire, storm, warning, volcano, quake, other)."),
				"source":      strProp("Optional source filter (jma_extra, mlit_river, gdacs, eonet)."),
				"limit":       intProp("Maximum number of records (default 100, max 200)."),
			}, nil),
		},
		{
			Name:        "get_river_levels",
			Description: "Get latest water level observations across monitored MLIT river stations, or query time-series water level history for a specific station_id.",
			InputSchema: objSchema(map[string]any{
				"station_id": strProp("Optional river station ID. When provided, returns time-series history and warning/danger thresholds for that station."),
				"hours":      intProp("Lookback window in hours when querying a specific station_id (default 72)."),
			}, nil),
		},
		{
			Name:        "analyze_environment_history",
			Description: "Analyze persisted long-term historical data with statistics (mean, min, max, median, stdDev, trendPerDay) and resampled time-series points for weather, marine, earthquakes, or disasters.",
			InputSchema: objSchema(map[string]any{
				"kind":        strProp("Dataset kind: weather, marine, quake, or disaster."),
				"location_id": strProp("Location ID for weather/marine queries (defaults to primary location if omitted)."),
				"metric":      strProp("Metric name (weather: temperature2m, pressureMsl, precipitation, rain, snowfall, snowDepth, cape, kpIndex, lightningCount, seaSurfaceTemp; marine: seaSurfaceTemperature, waveHeight, oceanCurrentVelocity)."),
				"from_time":   strProp("Start time (RFC3339 or YYYY-MM-DD or YYYYMMDDHHmm)."),
				"to_time":     strProp("End time (RFC3339 or YYYY-MM-DD or YYYYMMDDHHmm)."),
				"granularity": strProp("Aggregation granularity: auto, hourly, daily, monthly, or yearly."),
				"latitude":    numProp("Center latitude for quake/disaster spatial queries."),
				"longitude":   numProp("Center longitude for quake/disaster spatial queries."),
				"radius_km":   numProp("Radius in km for quake/disaster spatial queries (<=0 means global)."),
			}, []string{"kind"}),
		},
		{
			Name:        "search_disaster_lore",
			Description: "Search locally cached GSI Natural Disaster Monuments (自然災害伝承碑) by coordinates/radius, category, or text query.",
			InputSchema: objSchema(map[string]any{
				"latitude":  numProp("Center latitude for radius search."),
				"longitude": numProp("Center longitude for radius search."),
				"radius_km": numProp("Search radius in km around latitude/longitude."),
				"category":  strProp("Optional category filter: all, flood, landslide, snow, tsunami, other."),
				"query":     strProp("Optional keyword matching monument name, prefecture, municipality, disaster kind, or summary."),
				"limit":     intProp("Maximum number of results (default 50, max 500)."),
			}, nil),
		},
		{
			Name:        "search_and_manage_locations",
			Description: "List registered observation cities/marine points, search cities via Open-Meteo Geocoding, add or remove a city/marine point, or switch the primary city/marine location.",
			InputSchema: objSchema(map[string]any{
				"action":      strProp("Action to perform: list, search_city, add_city, remove_city, remove_marine, set_primary_city, or set_primary_marine."),
				"query":       strProp("City search query (required for search_city)."),
				"lang":        strProp("Language code for search_city (e.g. ja, en)."),
				"name":        strProp("City display name (for add_city)."),
				"latitude":    numProp("City latitude (required for add_city)."),
				"longitude":   numProp("City longitude (required for add_city)."),
				"location_id": strProp("Target location ID (for remove_city, remove_marine, set_primary_city, or set_primary_marine)."),
			}, []string{"action"}),
		},
		{
			Name:        "trigger_data_refresh",
			Description: "Trigger an immediate background data refresh across all collectors and return current history sync statuses.",
			InputSchema: objSchema(map[string]any{
				"sync_lore": boolProp("If true, also synchronize GSI Natural Disaster Monuments."),
			}, nil),
		},
		{
			Name:        "get_mcp_and_system_status",
			Description: "Get twearth system status, version, platform info, MCP server status, registered location counts, and historical data sync progress.",
			InputSchema: objSchema(nil, nil),
		},
	}
}

func (s *desktopService) executeTool(ctx context.Context, name string, rawArgs json.RawMessage) (toolCallResult, *jsonRPCError) {
	switch name {
	case "get_earth_summary":
		return s.toolGetEarthSummary(), nil
	case "get_current_weather":
		return s.toolGetCurrentWeather(rawArgs), nil
	case "get_current_marine":
		return s.toolGetCurrentMarine(rawArgs), nil
	case "get_synoptic_and_space":
		return s.toolGetSynopticAndSpace(rawArgs), nil
	case "list_alert_events":
		return s.toolListAlertEvents(rawArgs), nil
	case "list_recent_quakes":
		return s.toolListRecentQuakes(rawArgs), nil
	case "list_volcanoes":
		return s.toolListVolcanoes(rawArgs), nil
	case "list_active_disasters":
		return s.toolListActiveDisasters(rawArgs), nil
	case "get_river_levels":
		return s.toolGetRiverLevels(rawArgs), nil
	case "analyze_environment_history":
		return s.toolAnalyzeEnvironmentHistory(rawArgs), nil
	case "search_disaster_lore":
		return s.toolSearchDisasterLore(rawArgs), nil
	case "search_and_manage_locations":
		return s.toolSearchAndManageLocations(ctx, rawArgs), nil
	case "trigger_data_refresh":
		return s.toolTriggerDataRefresh(ctx, rawArgs), nil
	case "get_mcp_and_system_status":
		return s.toolGetMCPAndSystemStatus(), nil
	default:
		return toolCallResult{}, &jsonRPCError{
			Code:    -32602,
			Message: fmt.Sprintf("unknown tool: %s", name),
		}
	}
}

func (s *desktopService) latestState() collector.LatestState {
	if s.deps.Collector == nil {
		return collector.LatestState{}
	}
	return s.deps.Collector.GetLatestState()
}

func (s *desktopService) loadConfig() store.Config {
	if s.deps.Store == nil {
		return store.DefaultConfig()
	}
	cfg, err := s.deps.Store.GetConfig()
	if err != nil {
		return store.DefaultConfig()
	}
	return cfg
}

func (s *desktopService) toolGetEarthSummary() toolCallResult {
	cfg := s.loadConfig()
	st := s.latestState()

	activeDisasters := 0
	for _, d := range st.Disasters {
		if d.IsActive {
			activeDisasters++
		}
	}
	elevatedVolcanoes := make([]collector.VolcanoInfo, 0)
	for _, v := range st.Volcanoes {
		if v.AlertLevel >= 2 {
			elevatedVolcanoes = append(elevatedVolcanoes, v)
		}
	}
	recentQuakes := st.Quakes
	if len(recentQuakes) > 10 {
		recentQuakes = recentQuakes[:10]
	}

	var synopticSummary any
	if st.Synoptic != nil {
		synopticSummary = map[string]any{
			"timestamp":    st.Synoptic.Timestamp,
			"isobarCount":  len(st.Synoptic.Isobars),
			"centersCount": len(st.Synoptic.Centers),
			"centers":      st.Synoptic.Centers,
		}
	}
	var lidenSummary any
	if st.Liden != nil {
		lidenSummary = map[string]any{
			"validTime":   st.Liden.ValidTime,
			"totalCount":  st.Liden.TotalCount,
			"nearbyCount": st.Liden.NearbyCount,
			"radiusKm":    st.Liden.RadiusKm,
		}
	}

	summary := map[string]any{
		"primaryLocation":   cfg.PrimaryLocation(),
		"primaryMarine":     cfg.PrimaryMarineLocation(),
		"weathers":          st.Weathers,
		"marines":           st.Marines,
		"spaceWeather":      st.Space,
		"lightning":         lidenSummary,
		"synoptic":          synopticSummary,
		"activeDisasters":   activeDisasters,
		"totalDisasters":    len(st.Disasters),
		"elevatedVolcanoes": elevatedVolcanoes,
		"recentQuakes":      recentQuakes,
		"riverStations":     len(st.Rivers),
		"loreCount":         st.LoreCount,
	}
	return toolJSONResult(summary)
}

func (s *desktopService) toolGetCurrentWeather(rawArgs json.RawMessage) toolCallResult {
	var args struct {
		LocationID string `json:"location_id"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	st := s.latestState()
	if args.LocationID == "" {
		return toolJSONResult(map[string]any{
			"count":    len(st.Weathers),
			"weathers": st.Weathers,
		})
	}
	for _, w := range st.Weathers {
		if w.LocationID == args.LocationID {
			return toolJSONResult(w)
		}
	}
	return toolErrorResult(fmt.Sprintf("weather data not found for location_id %q", args.LocationID))
}

func (s *desktopService) toolGetCurrentMarine(rawArgs json.RawMessage) toolCallResult {
	var args struct {
		LocationID string `json:"location_id"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	st := s.latestState()
	if args.LocationID == "" {
		return toolJSONResult(map[string]any{
			"count":   len(st.Marines),
			"marines": st.Marines,
		})
	}
	for _, m := range st.Marines {
		if m.LocationID == args.LocationID {
			return toolJSONResult(m)
		}
	}
	return toolErrorResult(fmt.Sprintf("marine data not found for location_id %q", args.LocationID))
}

func (s *desktopService) toolGetSynopticAndSpace(rawArgs json.RawMessage) toolCallResult {
	var args struct {
		IncludeWindGrid bool `json:"include_wind_grid"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	st := s.latestState()
	var maxJet250, minPressure, maxPressure float64
	if len(st.WindGrid) > 0 {
		minPressure = st.WindGrid[0].PressureMSL
		maxPressure = st.WindGrid[0].PressureMSL
		for _, pt := range st.WindGrid {
			if pt.WindSpeed250hPa > maxJet250 {
				maxJet250 = pt.WindSpeed250hPa
			}
			if pt.PressureMSL < minPressure {
				minPressure = pt.PressureMSL
			}
			if pt.PressureMSL > maxPressure {
				maxPressure = pt.PressureMSL
			}
		}
	}

	out := map[string]any{
		"space": st.Space,
		"liden": st.Liden,
		"windGridSummary": map[string]any{
			"pointCount":     len(st.WindGrid),
			"maxJet250hPa":   maxJet250,
			"minPressureMsl": minPressure,
			"maxPressureMsl": maxPressure,
		},
	}
	if st.Synoptic != nil {
		out["synoptic"] = map[string]any{
			"timestamp":   st.Synoptic.Timestamp,
			"isobarCount": len(st.Synoptic.Isobars),
			"centers":     st.Synoptic.Centers,
		}
	}
	if args.IncludeWindGrid {
		out["windGrid"] = st.WindGrid
	}
	return toolJSONResult(out)
}

func (s *desktopService) toolListAlertEvents(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is unavailable")
	}
	var args struct {
		Hours       *int   `json:"hours"`
		Type        string `json:"type"`
		MinSeverity string `json:"min_severity"`
		Limit       int    `json:"limit"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	hours := 168
	if args.Hours != nil {
		hours = *args.Hours
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	var since time.Time
	if hours > 0 {
		since = time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	}
	events, err := s.deps.Store.ListEventsSince(since, 1000)
	if err != nil {
		return toolErrorResult(err.Error())
	}

	minRank := severityRank(args.MinSeverity)
	filtered := make([]store.EventRecord, 0, min(limit, len(events)))
	for _, ev := range events {
		if args.Type != "" && !strings.EqualFold(ev.Type, args.Type) {
			continue
		}
		if minRank > 0 && severityRank(ev.Severity) < minRank {
			continue
		}
		filtered = append(filtered, ev)
		if len(filtered) >= limit {
			break
		}
	}
	return toolJSONResult(map[string]any{
		"count":  len(filtered),
		"events": filtered,
	})
}

func (s *desktopService) toolListRecentQuakes(rawArgs json.RawMessage) toolCallResult {
	var args struct {
		MinMagnitude float64 `json:"min_magnitude"`
		MinIntensity string  `json:"min_intensity"`
		Latitude     float64 `json:"latitude"`
		Longitude    float64 `json:"longitude"`
		RadiusKm     float64 `json:"radius_km"`
		Limit        int     `json:"limit"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	st := s.latestState()
	minRank := collector.IntensityRank(args.MinIntensity)
	filtered := make([]collector.QuakeEvent, 0, min(limit, len(st.Quakes)))
	for _, q := range st.Quakes {
		if args.MinMagnitude > 0 && q.Magnitude < args.MinMagnitude {
			continue
		}
		if minRank > 0 && collector.IntensityRank(q.MaxScale) < minRank {
			continue
		}
		if args.RadiusKm > 0 {
			if collector.HaversineKm(args.Latitude, args.Longitude, q.Latitude, q.Longitude) > args.RadiusKm {
				continue
			}
		}
		filtered = append(filtered, q)
		if len(filtered) >= limit {
			break
		}
	}
	return toolJSONResult(map[string]any{
		"count":  len(filtered),
		"quakes": filtered,
	})
}

func (s *desktopService) toolListVolcanoes(rawArgs json.RawMessage) toolCallResult {
	var args struct {
		MinAlertLevel *int `json:"min_alert_level"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	minLevel := 1
	if args.MinAlertLevel != nil {
		minLevel = *args.MinAlertLevel
	}

	st := s.latestState()
	out := make([]collector.VolcanoInfo, 0)
	for _, v := range st.Volcanoes {
		if v.AlertLevel >= minLevel {
			out = append(out, v)
		}
	}
	return toolJSONResult(map[string]any{
		"count":     len(out),
		"volcanoes": out,
	})
}

func (s *desktopService) toolListActiveDisasters(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is unavailable")
	}
	var args struct {
		ActiveOnly *bool  `json:"active_only"`
		Category   string `json:"category"`
		Source     string `json:"source"`
		Limit      int    `json:"limit"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	activeOnly := true
	if args.ActiveOnly != nil {
		activeOnly = *args.ActiveOnly
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}

	recs, err := s.deps.Store.ListRecentDisasters(200, activeOnly)
	if err != nil {
		return toolErrorResult(err.Error())
	}
	filtered := make([]store.DisasterRecord, 0, min(limit, len(recs)))
	for _, r := range recs {
		if args.Category != "" && !strings.EqualFold(r.Category, args.Category) {
			continue
		}
		if args.Source != "" && !strings.EqualFold(r.Source, args.Source) {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) >= limit {
			break
		}
	}
	return toolJSONResult(map[string]any{
		"count":     len(filtered),
		"disasters": filtered,
	})
}

func (s *desktopService) toolGetRiverLevels(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is unavailable")
	}
	var args struct {
		StationID string `json:"station_id"`
		Hours     int    `json:"hours"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	if args.StationID != "" {
		hours := args.Hours
		if hours <= 0 {
			hours = 72
		}
		now := time.Now().UTC()
		res, err := s.deps.Store.QueryRiverHistory(store.RiverHistoryQuery{
			StationID: args.StationID,
			FromTime:  now.Add(-time.Duration(hours) * time.Hour),
			ToTime:    now,
		})
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(res)
	}

	stations, err := s.deps.Store.ListLatestRiverStations()
	if err != nil {
		return toolErrorResult(err.Error())
	}
	return toolJSONResult(map[string]any{
		"count":    len(stations),
		"stations": stations,
	})
}

func (s *desktopService) toolAnalyzeEnvironmentHistory(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is unavailable")
	}
	var args struct {
		Kind        string  `json:"kind"`
		LocationID  string  `json:"location_id"`
		Metric      string  `json:"metric"`
		FromTime    string  `json:"from_time"`
		ToTime      string  `json:"to_time"`
		Granularity string  `json:"granularity"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		RadiusKm    float64 `json:"radius_km"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(err.Error())
	}

	cfg := s.loadConfig()
	switch strings.ToLower(args.Kind) {
	case "weather", "marine":
		locID := args.LocationID
		if locID == "" {
			if args.Kind == "marine" {
				locID = cfg.PrimaryMarineLocation().ID
			} else {
				locID = cfg.PrimaryLocation().ID
			}
		}
		res, err := s.deps.Store.QueryHistory(store.HistoryQuery{
			Kind:        strings.ToLower(args.Kind),
			LocationID:  locID,
			FromTS:      normalizeTSParam(args.FromTime, false),
			ToTS:        normalizeTSParam(args.ToTime, true),
			Metric:      args.Metric,
			Granularity: args.Granularity,
		})
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(res)

	case "quake":
		lat, lon := args.Latitude, args.Longitude
		if lat == 0 && lon == 0 && args.RadiusKm > 0 {
			p := cfg.PrimaryLocation()
			lat, lon = p.Latitude, p.Longitude
		}
		fromT, _ := parseFlexibleTime(args.FromTime, false)
		toT, _ := parseFlexibleTime(args.ToTime, true)
		res, err := s.deps.Store.QueryQuakeHistory(store.QuakeHistoryQuery{
			Latitude:    lat,
			Longitude:   lon,
			RadiusKm:    args.RadiusKm,
			FromTime:    fromT,
			ToTime:      toT,
			Granularity: args.Granularity,
		})
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(res)

	case "disaster":
		lat, lon := args.Latitude, args.Longitude
		if lat == 0 && lon == 0 && args.RadiusKm > 0 {
			p := cfg.PrimaryLocation()
			lat, lon = p.Latitude, p.Longitude
		}
		fromT, _ := parseFlexibleTime(args.FromTime, false)
		toT, _ := parseFlexibleTime(args.ToTime, true)
		res, err := s.deps.Store.QueryDisasterHistory(store.DisasterHistoryQuery{
			Latitude:    lat,
			Longitude:   lon,
			RadiusKm:    args.RadiusKm,
			FromTime:    fromT,
			ToTime:      toT,
			Granularity: args.Granularity,
		})
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(res)

	default:
		return toolErrorResult("kind must be one of: weather, marine, quake, disaster")
	}
}

func (s *desktopService) toolSearchDisasterLore(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is unavailable")
	}
	var args struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		RadiusKm  float64 `json:"radius_km"`
		Category  string  `json:"category"`
		Query     string  `json:"query"`
		Limit     int     `json:"limit"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	items, err := s.deps.Store.QueryDisasterLore(store.DisasterLoreQuery{
		Latitude:  args.Latitude,
		Longitude: args.Longitude,
		RadiusKm:  args.RadiusKm,
		Category:  args.Category,
		Query:     args.Query,
		Limit:     limit,
	})
	if err != nil {
		return toolErrorResult(err.Error())
	}
	return toolJSONResult(map[string]any{
		"count": len(items),
		"lore":  items,
	})
}

func (s *desktopService) toolSearchAndManageLocations(ctx context.Context, rawArgs json.RawMessage) toolCallResult {
	var args struct {
		Action     string   `json:"action"`
		Query      string   `json:"query"`
		Lang       string   `json:"lang"`
		Name       string   `json:"name"`
		Latitude   *float64 `json:"latitude"`
		Longitude  *float64 `json:"longitude"`
		LocationID string   `json:"location_id"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(err.Error())
	}

	cfg := s.loadConfig()
	switch args.Action {
	case "list":
		return toolJSONResult(map[string]any{
			"cities":          cfg.Locations,
			"marineLocations": cfg.MarineLocations,
		})

	case "search_city":
		if strings.TrimSpace(args.Query) == "" {
			return toolErrorResult("query is required for search_city")
		}
		if s.deps.Collector == nil {
			return toolErrorResult("collector is unavailable")
		}
		lang := args.Lang
		if lang == "" {
			lang = "ja"
		}
		res, err := s.deps.Collector.SearchCities(ctx, args.Query, lang)
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{
			"count":   len(res),
			"results": res,
		})

	case "add_city":
		if s.deps.Store == nil {
			return toolErrorResult("store is unavailable")
		}
		if args.Latitude == nil || args.Longitude == nil {
			return toolErrorResult("latitude and longitude are required for add_city")
		}
		name := strings.TrimSpace(args.Name)
		if name == "" {
			name = fmt.Sprintf("%.2f, %.2f", *args.Latitude, *args.Longitude)
		}
		loc := store.ObservationLocation{
			ID:        fmt.Sprintf("mcp-%d", time.Now().UnixNano()),
			Name:      name,
			NameEn:    name,
			Latitude:  *args.Latitude,
			Longitude: *args.Longitude,
			Source:    "custom",
			IsPrimary: len(cfg.Locations) == 0,
			Order:     len(cfg.Locations),
		}
		cfg.Locations = append(cfg.Locations, loc)
		if err := s.persistAndNotifyConfig(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{
			"added":     loc,
			"locations": cfg.Locations,
		})

	case "remove_city":
		if s.deps.Store == nil {
			return toolErrorResult("store is unavailable")
		}
		if args.LocationID == "" {
			return toolErrorResult("location_id is required for remove_city")
		}
		idx := -1
		for i, loc := range cfg.Locations {
			if loc.ID == args.LocationID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return toolErrorResult(fmt.Sprintf("city location_id %q not found", args.LocationID))
		}
		if len(cfg.Locations) <= 1 {
			return toolErrorResult("cannot remove the last remaining city")
		}
		removed := cfg.Locations[idx]
		cfg.Locations = append(cfg.Locations[:idx], cfg.Locations[idx+1:]...)
		for i := range cfg.Locations {
			cfg.Locations[i].Order = i
		}
		if removed.IsPrimary && len(cfg.Locations) > 0 {
			cfg.Locations[0].IsPrimary = true
		}
		if err := s.persistAndNotifyConfig(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		updated := s.loadConfig()
		return toolJSONResult(map[string]any{
			"removed":     removed,
			"locations":   updated.Locations,
			"primaryCity": updated.PrimaryLocation(),
		})

	case "remove_marine":
		if s.deps.Store == nil {
			return toolErrorResult("store is unavailable")
		}
		if args.LocationID == "" {
			return toolErrorResult("location_id is required for remove_marine")
		}
		idx := -1
		for i, loc := range cfg.MarineLocations {
			if loc.ID == args.LocationID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return toolErrorResult(fmt.Sprintf("marine location_id %q not found", args.LocationID))
		}
		if len(cfg.MarineLocations) <= 1 {
			return toolErrorResult("cannot remove the last remaining marine location")
		}
		removed := cfg.MarineLocations[idx]
		cfg.MarineLocations = append(cfg.MarineLocations[:idx], cfg.MarineLocations[idx+1:]...)
		for i := range cfg.MarineLocations {
			cfg.MarineLocations[i].Order = i
		}
		if removed.IsPrimary && len(cfg.MarineLocations) > 0 {
			cfg.MarineLocations[0].IsPrimary = true
		}
		if err := s.persistAndNotifyConfig(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		updated := s.loadConfig()
		return toolJSONResult(map[string]any{
			"removed":         removed,
			"marineLocations": updated.MarineLocations,
			"primaryMarine":   updated.PrimaryMarineLocation(),
		})

	case "set_primary_city":
		if s.deps.Store == nil {
			return toolErrorResult("store is unavailable")
		}
		if args.LocationID == "" {
			return toolErrorResult("location_id is required for set_primary_city")
		}
		found := false
		for i := range cfg.Locations {
			if cfg.Locations[i].ID == args.LocationID {
				cfg.Locations[i].IsPrimary = true
				found = true
			} else {
				cfg.Locations[i].IsPrimary = false
			}
		}
		if !found {
			return toolErrorResult(fmt.Sprintf("city location_id %q not found", args.LocationID))
		}
		if err := s.persistAndNotifyConfig(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{
			"primaryCity": cfg.PrimaryLocation(),
		})

	case "set_primary_marine":
		if s.deps.Store == nil {
			return toolErrorResult("store is unavailable")
		}
		if args.LocationID == "" {
			return toolErrorResult("location_id is required for set_primary_marine")
		}
		found := false
		for i := range cfg.MarineLocations {
			if cfg.MarineLocations[i].ID == args.LocationID {
				cfg.MarineLocations[i].IsPrimary = true
				found = true
			} else {
				cfg.MarineLocations[i].IsPrimary = false
			}
		}
		if !found {
			return toolErrorResult(fmt.Sprintf("marine location_id %q not found", args.LocationID))
		}
		if err := s.persistAndNotifyConfig(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{
			"primaryMarine": cfg.PrimaryMarineLocation(),
		})

	default:
		return toolErrorResult("action must be one of: list, search_city, add_city, remove_city, remove_marine, set_primary_city, set_primary_marine")
	}
}

func (s *desktopService) persistAndNotifyConfig(cfg store.Config) error {
	if err := s.deps.Store.SaveConfig(cfg); err != nil {
		return err
	}
	saved, err := s.deps.Store.GetConfig()
	if err != nil {
		saved = cfg
	}
	if s.deps.OnConfigUpdated != nil {
		s.deps.OnConfigUpdated(saved)
	}
	if s.deps.Collector != nil {
		s.deps.Collector.TriggerRefresh()
		s.deps.Collector.TriggerHistorySync()
	}
	return nil
}

func (s *desktopService) toolTriggerDataRefresh(ctx context.Context, rawArgs json.RawMessage) toolCallResult {
	var args struct {
		SyncLore bool `json:"sync_lore"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	if s.deps.Collector == nil {
		return toolErrorResult("collector is unavailable")
	}
	s.deps.Collector.TriggerRefresh()
	s.deps.Collector.TriggerHistorySync()

	var loreCount *int
	if args.SyncLore {
		cnt, err := s.deps.Collector.SyncDisasterLore(ctx)
		if err != nil {
			return toolErrorResult(fmt.Sprintf("refresh triggered, but lore sync failed: %v", err))
		}
		loreCount = &cnt
	}

	out := map[string]any{
		"triggered":    true,
		"syncStatuses": s.deps.Collector.GetHistorySyncStatus(),
	}
	if loreCount != nil {
		out["loreCount"] = *loreCount
	}
	return toolJSONResult(out)
}

func (s *desktopService) toolGetMCPAndSystemStatus() toolCallResult {
	cfg := s.loadConfig()
	pInfo := platform.GetInfo()
	var syncStatuses []collector.HistorySyncStatus
	if s.deps.Collector != nil {
		syncStatuses = s.deps.Collector.GetHistorySyncStatus()
	}
	return toolJSONResult(map[string]any{
		"version":         s.deps.Version,
		"platform":        pInfo,
		"mcp":             s.GetStatus(),
		"cityCount":       len(cfg.Locations),
		"marineCount":     len(cfg.MarineLocations),
		"primaryLocation": cfg.PrimaryLocation(),
		"syncStatuses":    syncStatuses,
	})
}

func objSchema(props map[string]any, required []string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func unmarshalArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func toolJSONResult(v any) toolCallResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolErrorResult(err.Error())
	}
	return toolCallResult{
		Content: []toolTextContent{{Type: "text", Text: string(b)}},
	}
}

func toolErrorResult(msg string) toolCallResult {
	return toolCallResult{
		Content: []toolTextContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func severityRank(sev string) int {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func normalizeTSParam(s string, isEnd bool) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 12 && isDigits(s) {
		return s
	}
	if t, ok := parseFlexibleTime(s, isEnd); ok {
		return t.UTC().Format("200601021504")
	}
	return s
}

func parseFlexibleTime(s string, isEnd bool) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if isEnd {
			return t.Add(23*time.Hour + 59*time.Minute + 59*time.Second).UTC(), true
		}
		return t.UTC(), true
	}
	if t, err := time.Parse("200601021504", s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("20060102", s); err == nil {
		if isEnd {
			return t.Add(23*time.Hour + 59*time.Minute + 59*time.Second).UTC(), true
		}
		return t.UTC(), true
	}
	return time.Time{}, false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
