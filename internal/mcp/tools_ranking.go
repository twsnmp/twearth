//go:build !android && !ios

package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

func rankingToolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "get_municipality_ranking_catalog",
			Description: "List available municipality safety & livability ranking factors, regions, prefectures, and custom locations.",
			InputSchema: objSchema(nil, nil),
		},
		{
			Name:        "manage_municipality_ranking",
			Description: "Control or inspect the municipality safety & livability ranking job (status, start, pause, resume, clear_cache, add_custom, remove_custom).",
			InputSchema: objSchema(map[string]any{
				"action":                strProp("Action: status, start, pause, resume, clear_cache, add_custom, or remove_custom."),
				"scope":                 strProp("Scope for start: japan_all, japan_region, japan_pref, global, custom, or all."),
				"region":                strProp("Region filter when scope is japan_region (e.g. kanto, chubu)."),
				"prefecture":            strProp("Prefecture filter when scope is japan_pref."),
				"range_key":             strProp("Historical range key: 1y, 3y, 5y, 10y, or all (default 5y)."),
				"weights":               numMapProp("Factor weights map (e.g. {\"climate\": 35, \"quake\": 25, \"disaster\": 20, \"lore\": 10, \"volcano\": 10})."),
				"ideal_temp_c":          numProp("Ideal mean annual temperature in Celsius (default 16.0)."),
				"penalize_extreme_heat": boolProp("Penalize days >= 35°C."),
				"penalize_extreme_cold": boolProp("Penalize freezing days < 0°C."),
				"penalize_heavy_snow":   boolProp("Penalize heavy annual snowfall."),
				"quake_radius_km":       numProp("Search radius in km for earthquakes (default 75)."),
				"disaster_radius_km":    numProp("Search radius in km for disasters (default 35)."),
				"lore_radius_km":        numProp("Search radius in km for disaster monuments (default 15)."),
				"fetch_missing_climate": boolProp("Fetch missing daily climate data from Open-Meteo in background."),
				"force_rerun":           boolProp("Recompute all targets ignoring cache."),
				"clear_climate":         boolProp("When action is clear_cache, also clear cached climate statistics."),
				"municipality_id":       strProp("Municipality ID (required for remove_custom)."),
				"name":                  strProp("Custom municipality name (required for add_custom)."),
				"name_en":               strProp("Custom municipality English name."),
				"country":               strProp("Custom municipality country."),
				"latitude":              numProp("Custom municipality latitude (required for add_custom)."),
				"longitude":             numProp("Custom municipality longitude (required for add_custom)."),
			}, []string{"action"}),
		},
		{
			Name:        "list_municipality_rankings",
			Description: "List or filter cached municipality safety & livability ranking results sorted by total score.",
			InputSchema: objSchema(map[string]any{
				"region":         strProp("Optional region filter."),
				"prefecture":     strProp("Optional prefecture filter (Japanese or English)."),
				"country":        strProp("Optional country filter."),
				"climate_source": strProp("Optional climate data source filter: direct or interpolated."),
				"query":          strProp("Optional substring matching municipality name or prefecture."),
				"min_score":      numProp("Optional minimum total score (0..100)."),
				"max_score":      numProp("Optional maximum total score (0..100)."),
				"order":          strProp("Sort order: top (highest score first, default) or bottom (lowest score first)."),
				"limit":          intProp("Maximum items to return (default 30, max 200)."),
				"offset":         intProp("Pagination offset (default 0)."),
			}, nil),
		},
		{
			Name:        "get_municipality_ranking_detail",
			Description: "Get detailed factor scores and supporting local evidence (nearby earthquakes, disasters, disaster lore monuments) for a specific municipality.",
			InputSchema: objSchema(map[string]any{
				"municipality_id": strProp("Exact municipality ID (e.g. from list_municipality_rankings)."),
				"query":           strProp("Municipality name search query if municipality_id is not known."),
				"evidence_limit":  intProp("Maximum nearby evidence records per category (default 10, max 30)."),
			}, nil),
		},
		{
			Name:        "compare_municipalities",
			Description: "Compare 2 to 10 municipalities side-by-side across factor scores and raw hazard/climate metrics, with optional what-if weight and climate preference sensitivity analysis.",
			InputSchema: objSchema(map[string]any{
				"municipality_ids":      strArrProp("List of 2 to 10 municipality IDs to compare."),
				"weights":               numMapProp("Optional what-if factor weights to test rank/score sensitivity."),
				"ideal_temp_c":          numProp("Optional what-if ideal mean temperature in Celsius."),
				"penalize_extreme_heat": boolProp("Optional what-if extreme heat penalty toggle."),
				"penalize_extreme_cold": boolProp("Optional what-if extreme cold penalty toggle."),
				"penalize_heavy_snow":   boolProp("Optional what-if heavy snow penalty toggle."),
				"range_key":             strProp("Optional rangeKey override for what-if re-evaluation."),
			}, []string{"municipality_ids"}),
		},
		{
			Name:        "get_analysis_overview",
			Description: "Get a consolidated overview of stored historical coverage, correlation discovery status with top findings, and municipality ranking status with top/bottom ranked locations.",
			InputSchema: objSchema(nil, nil),
		},
	}
}

func (s *desktopService) toolGetMunicipalityRankingCatalog() toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is not initialized")
	}
	return toolJSONResult(analysis.BuildRankingCatalog(s.deps.Store))
}

func (s *desktopService) toolManageMunicipalityRanking(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Ranking == nil {
		return toolErrorResult("municipality ranking manager is not initialized")
	}
	var args struct {
		Action              string             `json:"action"`
		Scope               string             `json:"scope"`
		Region              string             `json:"region"`
		Prefecture          string             `json:"prefecture"`
		RangeKey            string             `json:"range_key"`
		Weights             map[string]float64 `json:"weights"`
		IdealTempC          *float64           `json:"ideal_temp_c"`
		PenalizeExtremeHeat *bool              `json:"penalize_extreme_heat"`
		PenalizeExtremeCold *bool              `json:"penalize_extreme_cold"`
		PenalizeHeavySnow   *bool              `json:"penalize_heavy_snow"`
		QuakeRadiusKm       float64            `json:"quake_radius_km"`
		DisasterRadiusKm    float64            `json:"disaster_radius_km"`
		LoreRadiusKm        float64            `json:"lore_radius_km"`
		FetchMissingClimate bool               `json:"fetch_missing_climate"`
		ForceRerun          bool               `json:"force_rerun"`
		ClearClimate        bool               `json:"clear_climate"`
		MunicipalityID      string             `json:"municipality_id"`
		Name                string             `json:"name"`
		NameEn              string             `json:"name_en"`
		Country             string             `json:"country"`
		Latitude            *float64           `json:"latitude"`
		Longitude           *float64           `json:"longitude"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "status":
		return toolJSONResult(s.deps.Ranking.Status())
	case "start":
		cur := s.deps.Ranking.Status().Config
		cfg := analysis.RankingConfig{
			Scope:               args.Scope,
			Region:              args.Region,
			Prefecture:          args.Prefecture,
			RangeKey:            args.RangeKey,
			Weights:             args.Weights,
			IdealTempC:          cur.IdealTempC,
			PenalizeExtremeHeat: cur.PenalizeExtremeHeat,
			PenalizeExtremeCold: cur.PenalizeExtremeCold,
			PenalizeHeavySnow:   cur.PenalizeHeavySnow,
			QuakeRadiusKm:       args.QuakeRadiusKm,
			DisasterRadiusKm:    args.DisasterRadiusKm,
			LoreRadiusKm:        args.LoreRadiusKm,
			FetchMissingClimate: args.FetchMissingClimate,
			ForceRerun:          args.ForceRerun,
		}
		if args.IdealTempC != nil {
			cfg.IdealTempC = *args.IdealTempC
		}
		if args.PenalizeExtremeHeat != nil {
			cfg.PenalizeExtremeHeat = *args.PenalizeExtremeHeat
		}
		if args.PenalizeExtremeCold != nil {
			cfg.PenalizeExtremeCold = *args.PenalizeExtremeCold
		}
		if args.PenalizeHeavySnow != nil {
			cfg.PenalizeHeavySnow = *args.PenalizeHeavySnow
		}
		if err := s.deps.Ranking.Start(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Ranking.Status())
	case "pause":
		if err := s.deps.Ranking.Pause(); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Ranking.Status())
	case "resume":
		if err := s.deps.Ranking.Resume(); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Ranking.Status())
	case "clear_cache":
		if err := s.deps.Ranking.ClearCache(args.ClearClimate); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{
			"cleared":      true,
			"clearClimate": args.ClearClimate,
			"status":       s.deps.Ranking.Status(),
		})
	case "add_custom":
		if s.deps.Store == nil {
			return toolErrorResult("store is not initialized")
		}
		if strings.TrimSpace(args.Name) == "" || args.Latitude == nil || args.Longitude == nil {
			return toolErrorResult("name, latitude, and longitude are required for add_custom")
		}
		entry, err := s.deps.Store.SaveCustomMunicipality(store.MunicipalityEntry{
			Name:       strings.TrimSpace(args.Name),
			NameEn:     strings.TrimSpace(args.NameEn),
			Prefecture: strings.TrimSpace(args.Prefecture),
			Country:    strings.TrimSpace(args.Country),
			Region:     strings.TrimSpace(args.Region),
			Latitude:   *args.Latitude,
			Longitude:  *args.Longitude,
		})
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(entry)
	case "remove_custom":
		if s.deps.Store == nil {
			return toolErrorResult("store is not initialized")
		}
		if strings.TrimSpace(args.MunicipalityID) == "" {
			return toolErrorResult("municipality_id is required for remove_custom")
		}
		if err := s.deps.Store.DeleteCustomMunicipality(strings.TrimSpace(args.MunicipalityID)); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{"removed": true, "municipalityId": args.MunicipalityID})
	default:
		return toolErrorResult(fmt.Sprintf("unsupported action %q", args.Action))
	}
}

func (s *desktopService) toolListMunicipalityRankings(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Ranking == nil {
		return toolErrorResult("municipality ranking manager is not initialized")
	}
	var args struct {
		Region        string   `json:"region"`
		Prefecture    string   `json:"prefecture"`
		Country       string   `json:"country"`
		ClimateSource string   `json:"climate_source"`
		Query         string   `json:"query"`
		MinScore      *float64 `json:"min_score"`
		MaxScore      *float64 `json:"max_score"`
		Order         string   `json:"order"`
		Limit         int      `json:"limit"`
		Offset        int      `json:"offset"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	items, err := s.deps.Ranking.ListResults()
	if err != nil {
		return toolErrorResult(err.Error())
	}

	q := strings.ToLower(strings.TrimSpace(args.Query))
	reg := strings.ToLower(strings.TrimSpace(args.Region))
	pref := strings.ToLower(strings.TrimSpace(args.Prefecture))
	country := strings.ToLower(strings.TrimSpace(args.Country))
	cs := strings.ToLower(strings.TrimSpace(args.ClimateSource))

	filtered := make([]analysis.RankingResultItem, 0, len(items))
	for _, it := range items {
		if reg != "" && !strings.EqualFold(it.Municipality.Region, reg) {
			continue
		}
		if pref != "" && !strings.EqualFold(it.Municipality.Prefecture, pref) && !strings.EqualFold(it.Municipality.PrefectureEn, pref) {
			continue
		}
		if country != "" && !strings.EqualFold(it.Municipality.Country, country) && !strings.EqualFold(it.Municipality.CountryEn, country) {
			continue
		}
		if cs != "" && !strings.EqualFold(it.ClimateSource, cs) {
			continue
		}
		if args.MinScore != nil && it.TotalScore < *args.MinScore {
			continue
		}
		if args.MaxScore != nil && it.TotalScore > *args.MaxScore {
			continue
		}
		if q != "" {
			hay := strings.ToLower(it.Municipality.Name + " " + it.Municipality.NameEn + " " + it.Municipality.Prefecture + " " + it.Municipality.PrefectureEn)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		filtered = append(filtered, it)
	}

	if strings.EqualFold(strings.TrimSpace(args.Order), "bottom") {
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].TotalScore != filtered[j].TotalScore {
				return filtered[i].TotalScore < filtered[j].TotalScore
			}
			return filtered[i].Rank > filtered[j].Rank
		})
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	offset := args.Offset
	if offset < 0 {
		offset = 0
	}
	totalMatched := len(filtered)
	if offset >= totalMatched {
		filtered = nil
	} else {
		end := offset + limit
		if end > totalMatched {
			end = totalMatched
		}
		filtered = filtered[offset:end]
	}

	return toolJSONResult(map[string]any{
		"totalCached":  len(items),
		"totalMatched": totalMatched,
		"offset":       offset,
		"limit":        limit,
		"count":        len(filtered),
		"items":        filtered,
	})
}

func (s *desktopService) toolGetMunicipalityRankingDetail(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil || s.deps.Ranking == nil {
		return toolErrorResult("store or ranking manager is not initialized")
	}
	var args struct {
		MunicipalityID string `json:"municipality_id"`
		Query          string `json:"query"`
		EvidenceLimit  int    `json:"evidence_limit"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.MunicipalityID) == "" && strings.TrimSpace(args.Query) == "" {
		return toolErrorResult("either municipality_id or query is required")
	}
	evLimit := args.EvidenceLimit
	if evLimit <= 0 {
		evLimit = 10
	}
	if evLimit > 30 {
		evLimit = 30
	}

	muni, found := s.findMunicipality(args.MunicipalityID, args.Query)
	if !found {
		return toolErrorResult("municipality not found")
	}

	prog := s.deps.Ranking.Status()
	cfg := prog.Config

	var item *analysis.RankingResultItem
	if cached, err := s.deps.Ranking.ListResults(); err == nil {
		for i := range cached {
			if cached[i].Municipality.ID == muni.ID {
				item = &cached[i]
				break
			}
		}
	}
	evaluatedOnDemand := false
	if item == nil {
		evals := s.deps.Ranking.EvaluateSubset(cfg, []store.MunicipalityEntry{muni})
		if len(evals) > 0 {
			item = &evals[0]
			item.Rank = 0 // not ranked against full cached set
			evaluatedOnDemand = true
		}
	}

	// Gather local supporting evidence around the municipality coordinates
	quakeRadius := cfg.QuakeRadiusKm
	if quakeRadius <= 0 {
		quakeRadius = 75
	}
	disasterRadius := cfg.DisasterRadiusKm
	if disasterRadius <= 0 {
		disasterRadius = 35
	}
	loreRadius := cfg.LoreRadiusKm
	if loreRadius <= 0 {
		loreRadius = 15
	}

	since := time.Now().UTC().AddDate(-5, 0, 0)
	if cfg.RangeKey == "1y" {
		since = time.Now().UTC().AddDate(-1, 0, 0)
	} else if cfg.RangeKey == "3y" {
		since = time.Now().UTC().AddDate(-3, 0, 0)
	} else if cfg.RangeKey == "10y" {
		since = time.Now().UTC().AddDate(-10, 0, 0)
	} else if cfg.RangeKey == "all" {
		since = time.Time{}
	}

	nearbyQuakes := make([]map[string]any, 0)
	if quakes, err := s.deps.Store.ListAllQuakesSince(since); err == nil {
		for _, q := range quakes {
			d := collector.HaversineKm(muni.Latitude, muni.Longitude, q.Latitude, q.Longitude)
			if d <= quakeRadius {
				nearbyQuakes = append(nearbyQuakes, map[string]any{
					"occurredAt": q.OccurredAt,
					"hypocenter": q.Hypocenter,
					"magnitude":  q.Magnitude,
					"depthKm":    q.DepthKm,
					"maxScale":   q.MaxScale,
					"distanceKm": math.Round(d*10) / 10,
				})
			}
		}
		sort.Slice(nearbyQuakes, func(i, j int) bool {
			return nearbyQuakes[i]["magnitude"].(float64) > nearbyQuakes[j]["magnitude"].(float64)
		})
		if len(nearbyQuakes) > evLimit {
			nearbyQuakes = nearbyQuakes[:evLimit]
		}
	}

	nearbyDisasters := make([]map[string]any, 0)
	if disasters, err := s.deps.Store.ListAllDisastersSince(since); err == nil {
		for _, dis := range disasters {
			d := collector.HaversineKm(muni.Latitude, muni.Longitude, dis.Latitude, dis.Longitude)
			if d <= disasterRadius {
				nearbyDisasters = append(nearbyDisasters, map[string]any{
					"source":     dis.Source,
					"category":   dis.Category,
					"severity":   dis.Severity,
					"title":      dis.Title,
					"areaName":   dis.AreaName,
					"startedAt":  dis.StartedAt,
					"isActive":   dis.IsActive,
					"distanceKm": math.Round(d*10) / 10,
				})
				if len(nearbyDisasters) >= evLimit {
					break
				}
			}
		}
	}

	nearbyLore, _ := s.deps.Store.QueryDisasterLore(store.DisasterLoreQuery{
		Latitude:  muni.Latitude,
		Longitude: muni.Longitude,
		RadiusKm:  loreRadius,
		Limit:     evLimit,
	})

	return toolJSONResult(map[string]any{
		"evaluatedOnDemand": evaluatedOnDemand,
		"configUsed":        cfg,
		"rankingItem":       item,
		"evidence": map[string]any{
			"quakeRadiusKm":    quakeRadius,
			"nearbyQuakes":     nearbyQuakes,
			"disasterRadiusKm": disasterRadius,
			"nearbyDisasters":  nearbyDisasters,
			"loreRadiusKm":     loreRadius,
			"nearbyLore":       nearbyLore,
		},
	})
}

func (s *desktopService) toolCompareMunicipalities(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil || s.deps.Ranking == nil {
		return toolErrorResult("store or ranking manager is not initialized")
	}
	var args struct {
		MunicipalityIDs     []string           `json:"municipality_ids"`
		Weights             map[string]float64 `json:"weights"`
		IdealTempC          *float64           `json:"ideal_temp_c"`
		PenalizeExtremeHeat *bool              `json:"penalize_extreme_heat"`
		PenalizeExtremeCold *bool              `json:"penalize_extreme_cold"`
		PenalizeHeavySnow   *bool              `json:"penalize_heavy_snow"`
		RangeKey            string             `json:"range_key"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(args.MunicipalityIDs) < 2 || len(args.MunicipalityIDs) > 10 {
		return toolErrorResult("municipality_ids must contain between 2 and 10 IDs")
	}

	allMunis, err := s.deps.Store.ListAllMunicipalities()
	if err != nil {
		return toolErrorResult(err.Error())
	}
	muniByID := make(map[string]store.MunicipalityEntry, len(allMunis))
	for _, m := range allMunis {
		muniByID[m.ID] = m
	}

	targets := make([]store.MunicipalityEntry, 0, len(args.MunicipalityIDs))
	missing := make([]string, 0)
	for _, id := range args.MunicipalityIDs {
		if m, ok := muniByID[strings.TrimSpace(id)]; ok {
			targets = append(targets, m)
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return toolErrorResult(fmt.Sprintf("unknown municipality_ids: %s", strings.Join(missing, ", ")))
	}

	baseCfg := s.deps.Ranking.Status().Config
	baseline := s.deps.Ranking.EvaluateSubset(baseCfg, targets)

	hasWhatIf := len(args.Weights) > 0 || args.IdealTempC != nil ||
		args.PenalizeExtremeHeat != nil || args.PenalizeExtremeCold != nil ||
		args.PenalizeHeavySnow != nil || strings.TrimSpace(args.RangeKey) != ""

	if !hasWhatIf {
		return toolJSONResult(map[string]any{
			"baselineConfig": baseCfg,
			"comparison":     baseline,
		})
	}

	whatIfCfg := baseCfg
	if len(args.Weights) > 0 {
		whatIfCfg.Weights = args.Weights
	}
	if args.IdealTempC != nil {
		whatIfCfg.IdealTempC = *args.IdealTempC
	}
	if args.PenalizeExtremeHeat != nil {
		whatIfCfg.PenalizeExtremeHeat = *args.PenalizeExtremeHeat
	}
	if args.PenalizeExtremeCold != nil {
		whatIfCfg.PenalizeExtremeCold = *args.PenalizeExtremeCold
	}
	if args.PenalizeHeavySnow != nil {
		whatIfCfg.PenalizeHeavySnow = *args.PenalizeHeavySnow
	}
	if strings.TrimSpace(args.RangeKey) != "" {
		whatIfCfg.RangeKey = strings.TrimSpace(args.RangeKey)
	}

	whatIf := s.deps.Ranking.EvaluateSubset(whatIfCfg, targets)
	baseMap := make(map[string]analysis.RankingResultItem, len(baseline))
	for _, b := range baseline {
		baseMap[b.Municipality.ID] = b
	}

	deltas := make([]map[string]any, 0, len(whatIf))
	for _, w := range whatIf {
		b := baseMap[w.Municipality.ID]
		deltas = append(deltas, map[string]any{
			"municipalityId":   w.Municipality.ID,
			"name":             w.Municipality.Name,
			"baselineRank":     b.Rank,
			"whatIfRank":       w.Rank,
			"rankDelta":        b.Rank - w.Rank, // positive means improved rank
			"baselineScore":    b.TotalScore,
			"whatIfScore":      w.TotalScore,
			"scoreDelta":       math.Round((w.TotalScore-b.TotalScore)*10) / 10,
			"climateSource":    w.ClimateSource,
			"whatIfFactors":    w.FactorScores,
			"baselineFactors":  b.FactorScores,
		})
	}

	return toolJSONResult(map[string]any{
		"baselineConfig": baseCfg,
		"whatIfConfig":   whatIfCfg,
		"sensitivity":    deltas,
	})
}

func (s *desktopService) toolGetAnalysisOverview() toolCallResult {
	cfg := s.loadConfig()

	out := map[string]any{
		"registeredCities":  len(cfg.Locations),
		"registeredMarines": len(cfg.MarineLocations),
	}
	if s.deps.Collector != nil {
		out["historySyncStatuses"] = s.deps.Collector.GetHistorySyncStatus()
	}
	if s.deps.Discovery != nil {
		prog := s.deps.Discovery.Status()
		cached, _ := s.deps.Discovery.ListCached()
		topN := cached
		if len(topN) > 5 {
			topN = topN[:5]
		}
		out["correlationDiscovery"] = map[string]any{
			"status":      prog,
			"cachedCount": len(cached),
			"topFindings": topN,
		}
	}
	if s.deps.Ranking != nil {
		prog := s.deps.Ranking.Status()
		results, _ := s.deps.Ranking.ListResults()
		topN := results
		if len(topN) > 5 {
			topN = topN[:5]
		}
		var bottomN []analysis.RankingResultItem
		if len(results) > 5 {
			bottomN = make([]analysis.RankingResultItem, 5)
			copy(bottomN, results[len(results)-5:])
		}
		out["municipalityRanking"] = map[string]any{
			"status":      prog,
			"cachedCount": len(results),
			"top5":        topN,
			"bottom5":     bottomN,
		}
	}
	return toolJSONResult(out)
}

func (s *desktopService) findMunicipality(id, query string) (store.MunicipalityEntry, bool) {
	all, err := s.deps.Store.ListAllMunicipalities()
	if err != nil {
		return store.MunicipalityEntry{}, false
	}
	id = strings.TrimSpace(id)
	if id != "" {
		for _, m := range all {
			if m.ID == id {
				return m, true
			}
		}
		return store.MunicipalityEntry{}, false
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return store.MunicipalityEntry{}, false
	}
	// Prefer exact match first, then substring match
	for _, m := range all {
		if strings.EqualFold(m.Name, q) || strings.EqualFold(m.NameEn, q) {
			return m, true
		}
	}
	for _, m := range all {
		if strings.Contains(strings.ToLower(m.Name), q) || strings.Contains(strings.ToLower(m.NameEn), q) {
			return m, true
		}
	}
	return store.MunicipalityEntry{}, false
}
