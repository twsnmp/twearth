package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

// RankingCatalog lists available evaluation factors, regions, prefectures, and total municipality counts.
type RankingCatalog struct {
	Factors            []RankingFactorDescriptor `json:"factors"`
	Regions            []string                  `json:"regions"`
	Prefectures        []string                  `json:"prefectures"`
	PrefecturesEn      []string                  `json:"prefecturesEn"`
	TotalJapanCount    int                       `json:"totalJapanCount"`
	TotalGlobalCount   int                       `json:"totalGlobalCount"`
	TotalCustomCount   int                       `json:"totalCustomCount"`
	CustomLocations    []store.MunicipalityEntry `json:"customLocations"`
}

// RankingConfig defines the user-selected scope, factor weights, and condition thresholds for municipality ranking.
type RankingConfig struct {
	Scope               string             `json:"scope"`               // "japan_all" | "japan_region" | "japan_pref" | "global" | "custom" | "all"
	Region              string             `json:"region,omitempty"`    // e.g. "kanto", "chubu", "europe"
	Prefecture          string             `json:"prefecture,omitempty"`
	RangeKey            string             `json:"rangeKey"`            // "1y" | "3y" | "5y" | "10y" | "all"
	Weights             map[string]float64 `json:"weights"`             // factorId -> weight (0..100)
	IdealTempC          float64            `json:"idealTempC"`          // e.g. 16.0
	PenalizeExtremeHeat bool               `json:"penalizeExtremeHeat"`
	PenalizeExtremeCold bool               `json:"penalizeExtremeCold"`
	PenalizeHeavySnow   bool               `json:"penalizeHeavySnow"`
	QuakeRadiusKm       float64            `json:"quakeRadiusKm"`       // default 75
	DisasterRadiusKm    float64            `json:"disasterRadiusKm"`    // default 35
	LoreRadiusKm        float64            `json:"loreRadiusKm"`        // default 15
	FetchMissingClimate bool               `json:"fetchMissingClimate"` // if true, fetches daily climate from Open-Meteo in background
	ForceRerun          bool               `json:"forceRerun"`
}

// RankingProgress reports live status of the municipality ranking analysis worker.
type RankingProgress struct {
	Status              string        `json:"status"` // "idle" | "running" | "paused" | "completed" | "error"
	Config              RankingConfig `json:"config"`
	Total               int           `json:"total"`
	Completed           int           `json:"completed"`
	CacheHits           int           `json:"cacheHits"`
	Computed            int           `json:"computed"`
	Skipped             int           `json:"skipped"`
	Errors              int           `json:"errors"`
	CurrentMunicipality string        `json:"currentMunicipality,omitempty"`
	LastError           string        `json:"lastError,omitempty"`
	StartedAt           *time.Time    `json:"startedAt,omitempty"`
	UpdatedAt           time.Time     `json:"updatedAt"`
}

// RankingResultItem represents a scored and ranked municipality entry with per-factor breakdowns.
type RankingResultItem struct {
	Rank           int                           `json:"rank"`
	Municipality   store.MunicipalityEntry       `json:"municipality"`
	TotalScore     float64                       `json:"totalScore"`
	FactorScores   map[string]RankingFactorScore `json:"factorScores"`
	MeanTempC      float64                       `json:"meanTempC"`
	AnnualSnowCm   float64                       `json:"annualSnowCm"`
	QuakeCount     int                           `json:"quakeCount"`
	QuakeMaxMag    float64                       `json:"quakeMaxMag"`
	DisasterCount  int                           `json:"disasterCount"`
	LoreCount      int                           `json:"loreCount"`
	VolcanoDistKm  float64                       `json:"volcanoDistKm"`
	ClimateSource  string                        `json:"climateSource"` // "direct" | "interpolated"
	UpdatedAt      time.Time                     `json:"updatedAt"`
}

// ClimateFetcher abstracts optional background Open-Meteo daily climate fetching for a municipality.
type ClimateFetcher func(ctx context.Context, loc store.ObservationLocation, startDate, endDate time.Time, useArchive bool) ([]store.DailyWeatherPoint, error)

// VolcanoProviderFunc returns the current active volcanoes list.
type VolcanoProviderFunc func() []collector.VolcanoInfo

// RankingManager orchestrates municipality safety/livability ranking, caching, pause/resume, and extensible factors.
type RankingManager struct {
	store          *store.Store
	climateFetcher ClimateFetcher
	volcanoGetter  VolcanoProviderFunc
	onProgress     func(RankingProgress)
	onResultBatch  func([]RankingResultItem)

	mu       sync.Mutex
	progress RankingProgress
	targets  []store.MunicipalityEntry
	cancel   context.CancelFunc
	doneCh   chan struct{}
}

// NewRankingManager creates a RankingManager and restores any saved state from store.
func NewRankingManager(
	st *store.Store,
	fetcher ClimateFetcher,
	volGetter VolcanoProviderFunc,
	onProgress func(RankingProgress),
	onResultBatch func([]RankingResultItem),
) *RankingManager {
	m := &RankingManager{
		store:          st,
		climateFetcher: fetcher,
		volcanoGetter:  volGetter,
		onProgress:     onProgress,
		onResultBatch:  onResultBatch,
		progress: RankingProgress{
			Status:    "idle",
			Config:    defaultRankingConfig(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	if st != nil {
		if saved, ok, err := st.LoadRankingState(); err == nil && ok {
			var cfg RankingConfig
			if len(saved.ConfigJSON) > 0 {
				_ = json.Unmarshal(saved.ConfigJSON, &cfg)
			}
			cfg = normalizeRankingConfig(cfg)
			status := saved.Status
			if status == "running" {
				status = "paused"
			}
			if status == "" {
				status = "idle"
			}
			m.progress = RankingProgress{
				Status:    status,
				Config:    cfg,
				Total:     saved.Total,
				Completed: saved.Cursor,
				CacheHits: saved.CacheHits,
				Computed:  saved.Computed,
				Skipped:   saved.Skipped,
				Errors:    saved.Errors,
				LastError: saved.LastError,
				UpdatedAt: saved.UpdatedAt,
			}
		}
	}
	return m
}

func defaultRankingConfig() RankingConfig {
	weights := make(map[string]float64)
	for _, p := range RegisteredRankingFactors() {
		d := p.Descriptor()
		weights[d.ID] = d.DefaultWeight
	}
	return RankingConfig{
		Scope:               "japan_all",
		RangeKey:            "5y",
		Weights:             weights,
		IdealTempC:          16.0,
		PenalizeExtremeHeat: true,
		PenalizeExtremeCold: true,
		PenalizeHeavySnow:   true,
		QuakeRadiusKm:       75.0,
		DisasterRadiusKm:    35.0,
		LoreRadiusKm:        15.0,
		FetchMissingClimate: false,
	}
}

func normalizeRankingConfig(cfg RankingConfig) RankingConfig {
	if cfg.Scope == "" {
		cfg.Scope = "japan_all"
	}
	if cfg.RangeKey == "" {
		cfg.RangeKey = "5y"
	}
	if cfg.Weights == nil {
		cfg.Weights = make(map[string]float64)
	}
	for _, p := range RegisteredRankingFactors() {
		d := p.Descriptor()
		if _, exists := cfg.Weights[d.ID]; !exists {
			cfg.Weights[d.ID] = d.DefaultWeight
		}
	}
	if cfg.IdealTempC == 0 {
		cfg.IdealTempC = 16.0
	}
	if cfg.QuakeRadiusKm <= 0 {
		cfg.QuakeRadiusKm = 75.0
	}
	if cfg.DisasterRadiusKm <= 0 {
		cfg.DisasterRadiusKm = 35.0
	}
	if cfg.LoreRadiusKm <= 0 {
		cfg.LoreRadiusKm = 15.0
	}
	return cfg
}

// BuildRankingCatalog returns metadata on registered factors, prefectures, regions, and custom municipalities.
func BuildRankingCatalog(st *store.Store) RankingCatalog {
	factors := RegisteredRankingFactors()
	descs := make([]RankingFactorDescriptor, 0, len(factors))
	for _, f := range factors {
		descs = append(descs, f.Descriptor())
	}

	var all []store.MunicipalityEntry
	var custom []store.MunicipalityEntry
	if st != nil {
		all, _ = st.ListAllMunicipalities()
		custom, _ = st.ListCustomMunicipalities()
	} else {
		all = store.BuiltinMunicipalities()
	}

	var jpCount, glCount, custCount int
	regionSet := make(map[string]bool)
	prefSeen := make(map[string]bool)
	var prefs, prefsEn []string

	for _, m := range all {
		switch m.Source {
		case "custom":
			custCount++
		default:
			if m.Group == "japan" {
				jpCount++
			} else {
				glCount++
			}
		}
		if m.Region != "" {
			regionSet[m.Region] = true
		}
		if m.Group == "japan" && m.Prefecture != "" && !prefSeen[m.Prefecture] {
			prefSeen[m.Prefecture] = true
			prefs = append(prefs, m.Prefecture)
			prefsEn = append(prefsEn, m.PrefectureEn)
		}
	}

	regions := []string{
		"hokkaido", "tohoku", "kanto", "chubu", "kinki", "chugoku", "shikoku", "kyushu",
		"asia", "north_america", "europe", "oceania", "latin_america", "middle_east", "africa",
	}

	return RankingCatalog{
		Factors:          descs,
		Regions:          regions,
		Prefectures:      prefs,
		PrefecturesEn:    prefsEn,
		TotalJapanCount:  jpCount,
		TotalGlobalCount: glCount,
		TotalCustomCount: len(custom),
		CustomLocations:  custom,
	}
}

// Status returns a snapshot of the current ranking progress.
func (m *RankingManager) Status() RankingProgress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.progress
}

// Start begins a municipality safety/livability ranking run with cfg.
func (m *RankingManager) Start(cfg RankingConfig) error {
	if m.store == nil {
		return fmt.Errorf("store is not initialized")
	}
	cfg = normalizeRankingConfig(cfg)
	all, err := m.store.ListAllMunicipalities()
	if err != nil {
		return err
	}
	targets := filterTargetMunicipalities(all, cfg)
	if len(targets) == 0 {
		return fmt.Errorf("no municipalities match the selected filter")
	}

	m.stopWorkerLocked()
	_ = m.store.ClearRankingCache(cfg.ForceRerun && cfg.FetchMissingClimate)

	now := time.Now().UTC()
	m.mu.Lock()
	m.targets = targets
	m.progress = RankingProgress{
		Status:    "running",
		Config:    cfg,
		Total:     len(targets),
		Completed: 0,
		StartedAt: &now,
		UpdatedAt: now,
	}
	m.persistStateLocked()
	prog := m.progress
	m.launchWorkerLocked(0)
	m.mu.Unlock()

	m.emitProgress(prog)
	return nil
}

// Resume continues a paused or interrupted ranking run from the saved cursor.
func (m *RankingManager) Resume() error {
	if m.store == nil {
		return fmt.Errorf("store is not initialized")
	}
	m.mu.Lock()
	if m.progress.Status == "running" {
		m.mu.Unlock()
		return nil
	}
	cfg := normalizeRankingConfig(m.progress.Config)
	cursor := m.progress.Completed
	targets := m.targets
	m.mu.Unlock()

	if len(targets) == 0 {
		all, err := m.store.ListAllMunicipalities()
		if err != nil {
			return err
		}
		targets = filterTargetMunicipalities(all, cfg)
	}
	if len(targets) == 0 || cursor >= len(targets) {
		m.mu.Lock()
		m.progress.Status = "completed"
		m.progress.UpdatedAt = time.Now().UTC()
		m.persistStateLocked()
		prog := m.progress
		m.mu.Unlock()
		m.emitProgress(prog)
		return nil
	}

	now := time.Now().UTC()
	m.mu.Lock()
	m.targets = targets
	m.progress.Status = "running"
	m.progress.Total = len(targets)
	m.progress.LastError = ""
	if m.progress.StartedAt == nil {
		m.progress.StartedAt = &now
	}
	m.progress.UpdatedAt = now
	m.persistStateLocked()
	prog := m.progress
	m.launchWorkerLocked(cursor)
	m.mu.Unlock()

	m.emitProgress(prog)
	return nil
}

// Pause stops the active ranking worker and saves the current cursor.
func (m *RankingManager) Pause() error {
	m.mu.Lock()
	if m.progress.Status != "running" {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	doneCh := m.doneCh
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
	return nil
}

// Shutdown pauses any active ranking worker cleanly on application exit.
func (m *RankingManager) Shutdown() {
	_ = m.Pause()
}

// ListResults returns all cached ranking results sorted by TotalScore descending with assigned 1-based ranks.
func (m *RankingManager) ListResults() ([]RankingResultItem, error) {
	if m.store == nil {
		return nil, nil
	}
	entries, err := m.store.ListRankingCacheEntries()
	if err != nil {
		return nil, err
	}
	out := make([]RankingResultItem, 0, len(entries))
	for _, e := range entries {
		var item RankingResultItem
		if err := json.Unmarshal(e.PayloadJSON, &item); err != nil {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalScore != out[j].TotalScore {
			return out[i].TotalScore > out[j].TotalScore
		}
		return out[i].Municipality.ID < out[j].Municipality.ID
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

// ClearCache deletes cached ranking results (and optionally climate cache).
func (m *RankingManager) ClearCache(clearClimate bool) error {
	if m.store == nil {
		return nil
	}
	if err := m.store.ClearRankingCache(clearClimate); err != nil {
		return err
	}
	m.mu.Lock()
	if m.progress.Status != "running" {
		m.progress.Total = 0
		m.progress.Completed = 0
		m.progress.CacheHits = 0
		m.progress.Computed = 0
		m.progress.Skipped = 0
		m.progress.Errors = 0
		m.progress.Status = "idle"
		m.progress.UpdatedAt = time.Now().UTC()
		m.persistStateLocked()
	}
	prog := m.progress
	m.mu.Unlock()
	m.emitProgress(prog)
	return nil
}

func (m *RankingManager) stopWorkerLocked() {
	m.mu.Lock()
	cancel := m.cancel
	doneCh := m.doneCh
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (m *RankingManager) launchWorkerLocked(startCursor int) {
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	m.cancel = cancel
	m.doneCh = doneCh

	targets := m.targets
	cfg := m.progress.Config
	go m.runWorker(ctx, doneCh, targets, cfg, startCursor)
}

func (m *RankingManager) runWorker(
	ctx context.Context,
	doneCh chan struct{},
	targets []store.MunicipalityEntry,
	cfg RankingConfig,
	startCursor int,
) {
	defer close(doneCh)

	evalCtx := m.buildEvalContext(cfg)
	factors := RegisteredRankingFactors()

	var pendingEntries []store.RankingCacheEntry
	var pendingItems []RankingResultItem
	lastEmit := time.Now()

	flushPending := func() {
		if len(pendingEntries) > 0 {
			_ = m.store.SaveRankingCacheBatch(pendingEntries)
			pendingEntries = pendingEntries[:0]
		}
		if len(pendingItems) > 0 && m.onResultBatch != nil {
			batch := make([]RankingResultItem, len(pendingItems))
			copy(batch, pendingItems)
			m.onResultBatch(batch)
			pendingItems = pendingItems[:0]
		}
	}

	for i := startCursor; i < len(targets); i++ {
		if ctx.Err() != nil {
			flushPending()
			m.mu.Lock()
			m.progress.Status = "paused"
			m.progress.Completed = i
			m.progress.CurrentMunicipality = ""
			m.progress.UpdatedAt = time.Now().UTC()
			m.persistStateLocked()
			prog := m.progress
			m.mu.Unlock()
			m.emitProgress(prog)
			return
		}

		muni := targets[i]
		muniLabel := muni.Name
		if muni.Prefecture != "" && muni.Prefecture != muni.Name {
			muniLabel = muni.Prefecture + " " + muni.Name
		}

		// Optional background climate fetch from Open-Meteo if enabled and not yet cached
		hasCachedClimate := false
		if st, ok := evalCtx.ClimateMap[muni.ID]; ok && st.SampleDays > 0 && st.Source == "direct" {
			hasCachedClimate = true
		}

		if cfg.FetchMissingClimate && !hasCachedClimate && m.climateFetcher != nil {
			endDate := evalCtx.Now.AddDate(0, 0, -5).Truncate(24 * time.Hour)
			startDate := evalCtx.Since
			if startDate.IsZero() || endDate.Sub(startDate) > 365*24*time.Hour {
				// Fetch 1-year representative daily climate summary per API call to stay fast and rate-limit friendly
				startDate = endDate.AddDate(-1, 0, 0)
			}
			pts, err := m.climateFetcher(ctx, store.ObservationLocation{
				ID:        muni.ID,
				Name:      muni.Name,
				Latitude:  muni.Latitude,
				Longitude: muni.Longitude,
			}, startDate, endDate, true)
			if err != nil {
				m.mu.Lock()
				m.progress.Errors++
				m.progress.LastError = err.Error()
				m.mu.Unlock()
			} else if len(pts) > 0 {
				st := store.SummarizeDailyWeatherPoints(muni.ID, cfg.RangeKey, pts, "direct")
				_ = m.store.SaveMunicipalityClimateStats(st)
				evalCtx.ClimateMap[muni.ID] = st
				evalCtx.AnchorPoints = append(evalCtx.AnchorPoints, climateAnchorPoint{
					Lat:   muni.Latitude,
					Lon:   muni.Longitude,
					Stats: st,
				})
			}
		}

		item := evaluateMunicipalityItem(evalCtx, factors, muni)
		rawItem, err := json.Marshal(item)
		if err == nil {
			pendingEntries = append(pendingEntries, store.RankingCacheEntry{
				ID:          muni.ID,
				TotalScore:  item.TotalScore,
				PayloadJSON: rawItem,
				UpdatedAt:   item.UpdatedAt,
			})
			pendingItems = append(pendingItems, item)
		}

		m.mu.Lock()
		m.progress.Completed = i + 1
		if hasCachedClimate {
			m.progress.CacheHits++
		} else {
			m.progress.Computed++
		}
		m.progress.CurrentMunicipality = muniLabel
		m.progress.UpdatedAt = time.Now().UTC()
		if (i+1)%100 == 0 || cfg.FetchMissingClimate {
			m.persistStateLocked()
		}
		prog := m.progress
		m.mu.Unlock()

		if len(pendingEntries) >= 120 || cfg.FetchMissingClimate || time.Since(lastEmit) >= 150*time.Millisecond || i == len(targets)-1 {
			flushPending()
			m.emitProgress(prog)
			lastEmit = time.Now()
		}
	}

	flushPending()
	m.mu.Lock()
	m.progress.Status = "completed"
	m.progress.Completed = len(targets)
	m.progress.CurrentMunicipality = ""
	m.progress.UpdatedAt = time.Now().UTC()
	m.persistStateLocked()
	prog := m.progress
	m.mu.Unlock()
	m.emitProgress(prog)
}

func (m *RankingManager) buildEvalContext(cfg RankingConfig) *RankingEvalContext {
	now := time.Now().UTC()
	since, years := rankingRangeToSince(cfg.RangeKey, now)

	var quakes []store.QuakeRecord
	var disasters []store.DisasterRecord
	var lore []store.DisasterLoreRecord
	var rivers []store.RiverTimeSeriesPoint
	climateMap := make(map[string]store.MunicipalityClimateStats)
	var anchors []climateAnchorPoint

	if m.store != nil {
		quakes, _ = m.store.ListAllQuakesSince(since)
		disasters, _ = m.store.ListAllDisastersSince(since)
		lore, _ = m.store.ListAllDisasterLore()
		rivers, _ = m.store.ListLatestRiverStations()
		if cm, err := m.store.ListMunicipalityClimateStats(cfg.RangeKey); err == nil && cm != nil {
			climateMap = cm
		}
		// Seed anchor points from configured observation locations that have stored weather history
		if storeCfg, err := m.store.GetConfig(); err == nil {
			for _, loc := range storeCfg.Locations {
				if st, ok := m.store.ComputeLocationClimateStats(loc.ID, cfg.RangeKey, since); ok {
					climateMap["obs-"+loc.ID] = st
					climateMap[loc.ID] = st
					anchors = append(anchors, climateAnchorPoint{
						Lat:   loc.Latitude,
						Lon:   loc.Longitude,
						Stats: st,
					})
				}
			}
		}
		// Also add any cached direct municipality stats as spatial anchors
		for id, st := range climateMap {
			if st.Source == "direct" && st.SampleDays > 0 {
				if bm, ok := store.GetBuiltinMunicipality(id); ok {
					anchors = append(anchors, climateAnchorPoint{
						Lat:   bm.Latitude,
						Lon:   bm.Longitude,
						Stats: st,
					})
				}
			}
		}
	}

	var volcanoes []collector.VolcanoInfo
	if m.volcanoGetter != nil {
		volcanoes = m.volcanoGetter()
	}
	if len(volcanoes) == 0 {
		volcanoes = defaultJapanVolcanoes()
	}

	evalCtx := &RankingEvalContext{
		Config:       cfg,
		Store:        m.store,
		Now:          now,
		Since:        since,
		YearsSpan:    years,
		Quakes:       quakes,
		Disasters:    disasters,
		Lore:         lore,
		Volcanoes:    volcanoes,
		Rivers:       rivers,
		ClimateMap:   climateMap,
		AnchorPoints: anchors,
	}
	evalCtx.buildSpatialGrids()
	return evalCtx
}

func evaluateMunicipalityItem(
	evalCtx *RankingEvalContext,
	factors []RankingFactorProvider,
	muni store.MunicipalityEntry,
) RankingResultItem {
	scores := make(map[string]RankingFactorScore, len(factors))
	var weightedSum, weightTotal float64

	for _, f := range factors {
		desc := f.Descriptor()
		fs := f.Evaluate(evalCtx, muni)
		scores[desc.ID] = fs
		w := evalCtx.Config.Weights[desc.ID]
		if w < 0 {
			w = 0
		}
		if fs.Applicable && w > 0 {
			weightedSum += fs.Score * w
			weightTotal += w
		}
	}

	totalScore := 0.0
	if weightTotal > 0 {
		totalScore = math.Round((weightedSum/weightTotal)*10) / 10
	} else if len(scores) > 0 {
		sum := 0.0
		cnt := 0
		for _, s := range scores {
			if s.Applicable {
				sum += s.Score
				cnt++
			}
		}
		if cnt > 0 {
			totalScore = math.Round((sum/float64(cnt))*10) / 10
		}
	}

	item := RankingResultItem{
		Municipality:  muni,
		TotalScore:    totalScore,
		FactorScores:  scores,
		ClimateSource: "interpolated",
		UpdatedAt:     time.Now().UTC(),
	}

	if cs, ok := evalCtx.ClimateMap[muni.ID]; ok && cs.Source == "direct" {
		item.ClimateSource = "direct"
	}
	if cf, ok := scores["climate"]; ok && cf.Metrics != nil {
		item.MeanTempC = cf.Metrics["meanTempC"]
		item.AnnualSnowCm = cf.Metrics["annualSnowCm"]
	}
	if qf, ok := scores["quake"]; ok && qf.Metrics != nil {
		item.QuakeCount = int(qf.Metrics["quakeCount"])
		item.QuakeMaxMag = qf.Metrics["maxMagnitude"]
	}
	if df, ok := scores["disaster"]; ok && df.Metrics != nil {
		item.DisasterCount = int(df.Metrics["disasterCount"])
	}
	if lf, ok := scores["lore"]; ok && lf.Metrics != nil {
		item.LoreCount = int(lf.Metrics["loreCount"])
	}
	if vf, ok := scores["volcano"]; ok && vf.Metrics != nil {
		item.VolcanoDistKm = vf.Metrics["nearestVolcanoKm"]
	}

	return item
}

func filterTargetMunicipalities(all []store.MunicipalityEntry, cfg RankingConfig) []store.MunicipalityEntry {
	var out []store.MunicipalityEntry
	prefFilter := strings.TrimSpace(cfg.Prefecture)
	regFilter := strings.TrimSpace(cfg.Region)

	for _, m := range all {
		switch cfg.Scope {
		case "japan_all":
			if m.Group != "japan" {
				continue
			}
		case "japan_region":
			if m.Group != "japan" {
				continue
			}
			if regFilter != "" && regFilter != "all" && !strings.EqualFold(m.Region, regFilter) {
				continue
			}
		case "japan_pref":
			if m.Group != "japan" {
				continue
			}
			if prefFilter != "" && prefFilter != "all" &&
				!strings.EqualFold(m.Prefecture, prefFilter) &&
				!strings.EqualFold(m.PrefectureEn, prefFilter) {
				continue
			}
		case "global":
			if m.Group != "global" {
				continue
			}
			if regFilter != "" && regFilter != "all" && !strings.EqualFold(m.Region, regFilter) {
				continue
			}
		case "custom":
			if m.Source != "custom" && m.Source != "observed" {
				continue
			}
		default: // "all"
			if regFilter != "" && regFilter != "all" && !strings.EqualFold(m.Region, regFilter) {
				continue
			}
			if prefFilter != "" && prefFilter != "all" &&
				!strings.EqualFold(m.Prefecture, prefFilter) &&
				!strings.EqualFold(m.PrefectureEn, prefFilter) {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

func rankingRangeToSince(rangeKey string, now time.Time) (time.Time, float64) {
	switch rangeKey {
	case "1y":
		return now.AddDate(-1, 0, 0), 1.0
	case "3y":
		return now.AddDate(-3, 0, 0), 3.0
	case "5y":
		return now.AddDate(-5, 0, 0), 5.0
	case "10y":
		return now.AddDate(-10, 0, 0), 10.0
	case "30y":
		return now.AddDate(-30, 0, 0), 30.0
	case "all":
		return time.Time{}, 10.0
	default:
		return now.AddDate(-5, 0, 0), 5.0
	}
}

func (m *RankingManager) persistStateLocked() {
	if m.store == nil {
		return
	}
	cfgJSON, _ := json.Marshal(m.progress.Config)
	_ = m.store.SaveRankingState(store.RankingSavedState{
		Status:     m.progress.Status,
		ConfigJSON: cfgJSON,
		Total:      m.progress.Total,
		Cursor:     m.progress.Completed,
		CacheHits:  m.progress.CacheHits,
		Computed:   m.progress.Computed,
		Skipped:    m.progress.Skipped,
		Errors:     m.progress.Errors,
		LastError:  m.progress.LastError,
		UpdatedAt:  m.progress.UpdatedAt,
	})
}

func (m *RankingManager) emitProgress(prog RankingProgress) {
	if m.onProgress != nil {
		m.onProgress(prog)
	}
}

// Built-in representative major active volcanoes in Japan & world as fallback when live volcano feed has not yet completed.
func defaultJapanVolcanoes() []collector.VolcanoInfo {
	return []collector.VolcanoInfo{
		{Code: "101", Name: "知床硫黄山", Latitude: 44.133, Longitude: 145.161, AlertLevel: 1},
		{Code: "105", Name: "雌阿寒岳", Latitude: 43.386, Longitude: 144.013, AlertLevel: 1},
		{Code: "108", Name: "十勝岳", Latitude: 43.418, Longitude: 142.686, AlertLevel: 1},
		{Code: "110", Name: "樽前山", Latitude: 42.690, Longitude: 141.376, AlertLevel: 1},
		{Code: "112", Name: "有珠山", Latitude: 42.544, Longitude: 140.839, AlertLevel: 1},
		{Code: "113", Name: "北海道駒ヶ岳", Latitude: 42.063, Longitude: 140.677, AlertLevel: 1},
		{Code: "202", Name: "岩木山", Latitude: 40.656, Longitude: 140.303, AlertLevel: 1},
		{Code: "205", Name: "岩手山", Latitude: 39.853, Longitude: 141.001, AlertLevel: 2},
		{Code: "207", Name: "鳥海山", Latitude: 39.099, Longitude: 140.049, AlertLevel: 1},
		{Code: "212", Name: "蔵王山", Latitude: 38.144, Longitude: 140.440, AlertLevel: 1},
		{Code: "214", Name: "吾妻山", Latitude: 37.735, Longitude: 140.244, AlertLevel: 1},
		{Code: "215", Name: "安達太良山", Latitude: 37.621, Longitude: 140.281, AlertLevel: 1},
		{Code: "216", Name: "磐梯山", Latitude: 37.601, Longitude: 140.072, AlertLevel: 1},
		{Code: "301", Name: "那須岳", Latitude: 37.125, Longitude: 139.963, AlertLevel: 1},
		{Code: "305", Name: "草津白根山", Latitude: 36.618, Longitude: 138.528, AlertLevel: 1},
		{Code: "306", Name: "浅間山", Latitude: 36.406, Longitude: 138.523, AlertLevel: 2},
		{Code: "307", Name: "新潟焼山", Latitude: 36.921, Longitude: 138.036, AlertLevel: 1},
		{Code: "311", Name: "焼岳", Latitude: 36.227, Longitude: 137.587, AlertLevel: 1},
		{Code: "312", Name: "御嶽山", Latitude: 35.893, Longitude: 137.480, AlertLevel: 1},
		{Code: "313", Name: "白山", Latitude: 36.155, Longitude: 136.771, AlertLevel: 1},
		{Code: "314", Name: "富士山", Latitude: 35.361, Longitude: 138.727, AlertLevel: 1},
		{Code: "315", Name: "箱根山", Latitude: 35.233, Longitude: 139.021, AlertLevel: 1},
		{Code: "317", Name: "伊豆大島", Latitude: 34.725, Longitude: 139.394, AlertLevel: 1},
		{Code: "320", Name: "三宅島", Latitude: 34.094, Longitude: 139.526, AlertLevel: 1},
		{Code: "502", Name: "鶴見岳・伽藍岳", Latitude: 33.287, Longitude: 131.432, AlertLevel: 1},
		{Code: "503", Name: "九重山", Latitude: 33.086, Longitude: 131.249, AlertLevel: 1},
		{Code: "504", Name: "阿蘇山", Latitude: 32.884, Longitude: 131.104, AlertLevel: 1},
		{Code: "505", Name: "雲仙岳", Latitude: 32.761, Longitude: 130.299, AlertLevel: 1},
		{Code: "506", Name: "霧島山", Latitude: 31.934, Longitude: 130.862, AlertLevel: 2},
		{Code: "507", Name: "桜島", Latitude: 31.585, Longitude: 130.657, AlertLevel: 3},
		{Code: "509", Name: "薩摩硫黄島", Latitude: 30.793, Longitude: 130.305, AlertLevel: 2},
		{Code: "510", Name: "口永良部島", Latitude: 30.443, Longitude: 130.217, AlertLevel: 2},
		{Code: "511", Name: "諏訪之瀬島", Latitude: 29.638, Longitude: 129.714, AlertLevel: 2},
	}
}
