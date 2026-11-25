package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// DiscoveryConfig defines the search space for automated background correlation discovery.
type DiscoveryConfig struct {
	Kinds               []string `json:"kinds,omitempty"`
	LocationIDs         []string `json:"locationIds,omitempty"`
	Metrics             []string `json:"metrics,omitempty"`
	RangeKey            string   `json:"rangeKey"`
	Granularity         string   `json:"granularity"`
	Algorithms          []string `json:"algorithms,omitempty"`
	DeseasonalizeModes  []bool   `json:"deseasonalizeModes,omitempty"`
	Difference          bool     `json:"difference"`
	MovingAvgWindow     int      `json:"movingAvgWindow"`
	MaxLag              int      `json:"maxLag"`
	MinSamples          int      `json:"minSamples"`
	SameLocationAllowed bool     `json:"sameLocationAllowed"`
	CrossKindOnly       bool     `json:"crossKindOnly"`
	ForceRerun          bool     `json:"forceRerun"`
}

// DiscoveryProgress reports live status of the background correlation discovery worker.
type DiscoveryProgress struct {
	Status           string          `json:"status"` // "idle" | "running" | "paused" | "completed" | "error"
	Config           DiscoveryConfig `json:"config"`
	Total            int             `json:"total"`
	Completed        int             `json:"completed"`
	CacheHits        int             `json:"cacheHits"`
	Computed         int             `json:"computed"`
	Skipped          int             `json:"skipped"`
	Errors           int             `json:"errors"`
	CurrentPair      string          `json:"currentPair,omitempty"`
	CurrentAlgorithm string          `json:"currentAlgorithm,omitempty"`
	LastError        string          `json:"lastError,omitempty"`
	StartedAt        *time.Time      `json:"startedAt,omitempty"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// DiscoveryResultItem represents a cached or newly discovered correlation pair summary.
type DiscoveryResultItem struct {
	Key         string             `json:"key"`
	Request     CorrelationRequest `json:"request"`
	Metrics     CorrelationMetrics `json:"metrics"`
	SampleCount int                `json:"sampleCount"`
	Score       float64            `json:"score"`
	Warnings    []string           `json:"warnings,omitempty"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type discoveryTask struct {
	key string
	req CorrelationRequest
}

// DiscoveryManager orchestrates background correlation screening, caching, pause/resume, and rerun.
type DiscoveryManager struct {
	store      *store.Store
	onProgress func(DiscoveryProgress)
	onResult   func(DiscoveryResultItem)

	mu       sync.Mutex
	progress DiscoveryProgress
	tasks    []discoveryTask
	cancel   context.CancelFunc
	doneCh   chan struct{}
}

// NewDiscoveryManager creates a DiscoveryManager and restores any saved pause/completion state from store.
func NewDiscoveryManager(st *store.Store, onProgress func(DiscoveryProgress), onResult func(DiscoveryResultItem)) *DiscoveryManager {
	m := &DiscoveryManager{
		store:      st,
		onProgress: onProgress,
		onResult:   onResult,
		progress: DiscoveryProgress{
			Status:    "idle",
			Config:    defaultDiscoveryConfig(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	if st != nil {
		if saved, ok, err := st.LoadDiscoveryState(); err == nil && ok {
			var cfg DiscoveryConfig
			if len(saved.ConfigJSON) > 0 {
				_ = json.Unmarshal(saved.ConfigJSON, &cfg)
			}
			cfg = normalizeDiscoveryConfig(cfg)
			status := saved.Status
			if status == "running" {
				// Previous run was interrupted by app exit; allow seamless resume.
				status = "paused"
			}
			if status == "" {
				status = "idle"
			}
			m.progress = DiscoveryProgress{
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

func defaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		RangeKey:            "5y",
		Granularity:         "monthly",
		Algorithms:          []string{"cross_correlation"},
		DeseasonalizeModes:  []bool{true},
		MaxLag:              12,
		MinSamples:          12,
		SameLocationAllowed: false,
	}
}

func normalizeDiscoveryConfig(cfg DiscoveryConfig) DiscoveryConfig {
	if cfg.RangeKey == "" {
		cfg.RangeKey = "5y"
	}
	if cfg.Granularity == "" {
		cfg.Granularity = "monthly"
	}
	if len(cfg.Algorithms) == 0 {
		cfg.Algorithms = []string{"cross_correlation"}
	} else {
		filtered := make([]string, 0, len(cfg.Algorithms))
		seen := make(map[string]bool)
		for _, a := range cfg.Algorithms {
			switch a {
			case "pearson", "spearman", "cross_correlation":
				if !seen[a] {
					seen[a] = true
					filtered = append(filtered, a)
				}
			}
		}
		if len(filtered) == 0 {
			filtered = []string{"pearson"}
		}
		cfg.Algorithms = filtered
	}
	if len(cfg.DeseasonalizeModes) == 0 {
		cfg.DeseasonalizeModes = []bool{true}
	} else if len(cfg.DeseasonalizeModes) > 2 {
		cfg.DeseasonalizeModes = cfg.DeseasonalizeModes[:2]
	}
	if cfg.MaxLag <= 0 {
		switch resolveGranularity(cfg.RangeKey, cfg.Granularity) {
		case "hourly":
			cfg.MaxLag = 24
		case "daily":
			cfg.MaxLag = 30
		default:
			cfg.MaxLag = 12
		}
	}
	if cfg.MinSamples < 3 {
		cfg.MinSamples = 12
	}
	return cfg
}

// CorrelationRequestKey computes a deterministic cache key for a CorrelationRequest.
func CorrelationRequestKey(req CorrelationRequest) string {
	gran := resolveGranularity(req.RangeKey, req.Granularity)
	alg := req.Algorithm
	if alg == "" {
		alg = "pearson"
	}
	maxLag := req.Params.MaxLag
	if alg != "cross_correlation" {
		maxLag = 0
	}
	canonical := fmt.Sprintf(
		"%s:%s:%s|%s:%s:%s|r=%s|g=%s|a=%s|d=%t|ma=%d|df=%t|st=%t|l=%d|ml=%d",
		req.SeriesX.Kind, req.SeriesX.LocationID, req.SeriesX.Metric,
		req.SeriesY.Kind, req.SeriesY.LocationID, req.SeriesY.Metric,
		req.RangeKey, gran, alg,
		req.Preprocess.Deseasonalize, req.Preprocess.MovingAvgWindow,
		req.Preprocess.Difference, req.Preprocess.Standardize,
		req.Preprocess.Lag, maxLag,
	)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:16])
}

// Status returns a snapshot of the current discovery progress.
func (m *DiscoveryManager) Status() DiscoveryProgress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.progress
}

// Start begins a fresh background correlation discovery run with cfg.
func (m *DiscoveryManager) Start(cfg DiscoveryConfig) error {
	if m.store == nil {
		return fmt.Errorf("store is not initialized")
	}
	cfg = normalizeDiscoveryConfig(cfg)

	storeCfg, err := m.store.GetConfig()
	if err != nil {
		return err
	}
	catalog := BuildCatalogWithStore(m.store, storeCfg)
	tasks := buildDiscoveryTasks(catalog, cfg)
	if len(tasks) == 0 {
		return fmt.Errorf("no valid series pairs match the discovery filter")
	}

	m.stopWorkerLocked()

	now := time.Now().UTC()
	m.mu.Lock()
	m.tasks = tasks
	m.progress = DiscoveryProgress{
		Status:    "running",
		Config:    cfg,
		Total:     len(tasks),
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

// Resume continues a paused or interrupted discovery run from the saved cursor.
func (m *DiscoveryManager) Resume() error {
	if m.store == nil {
		return fmt.Errorf("store is not initialized")
	}
	m.mu.Lock()
	if m.progress.Status == "running" {
		m.mu.Unlock()
		return nil
	}
	cfg := normalizeDiscoveryConfig(m.progress.Config)
	cursor := m.progress.Completed
	tasks := m.tasks
	m.mu.Unlock()

	if len(tasks) == 0 {
		storeCfg, err := m.store.GetConfig()
		if err != nil {
			return err
		}
		catalog := BuildCatalogWithStore(m.store, storeCfg)
		tasks = buildDiscoveryTasks(catalog, cfg)
	}
	if len(tasks) == 0 || cursor >= len(tasks) {
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
	m.tasks = tasks
	m.progress.Status = "running"
	m.progress.Total = len(tasks)
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

// Pause stops the active background discovery worker and persists the current cursor.
func (m *DiscoveryManager) Pause() error {
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

// Shutdown pauses any active discovery worker cleanly on application exit.
func (m *DiscoveryManager) Shutdown() {
	_ = m.Pause()
}

// ListCached returns all remembered correlation discovery results sorted by score descending.
func (m *DiscoveryManager) ListCached() ([]DiscoveryResultItem, error) {
	if m.store == nil {
		return nil, nil
	}
	entries, err := m.store.ListCorrelationCacheEntries()
	if err != nil {
		return nil, err
	}
	out := make([]DiscoveryResultItem, 0, len(entries))
	for _, e := range entries {
		item, err := entryToItem(e)
		if err != nil {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// GetCached returns a single remembered correlation result by key.
func (m *DiscoveryManager) GetCached(key string) (DiscoveryResultItem, bool, error) {
	if m.store == nil {
		return DiscoveryResultItem{}, false, nil
	}
	e, ok, err := m.store.GetCorrelationCacheEntry(key)
	if err != nil || !ok {
		return DiscoveryResultItem{}, ok, err
	}
	item, err := entryToItem(e)
	if err != nil {
		return DiscoveryResultItem{}, false, err
	}
	return item, true, nil
}

// DeleteCached deletes specific remembered entries by key.
func (m *DiscoveryManager) DeleteCached(keys []string) (int, error) {
	if m.store == nil {
		return 0, nil
	}
	return m.store.DeleteCorrelationCacheEntries(keys)
}

// ClearCached removes all remembered correlation entries and resets idle status if not running.
func (m *DiscoveryManager) ClearCached() error {
	if m.store == nil {
		return nil
	}
	if err := m.store.ClearCorrelationCache(); err != nil {
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

// RerunEntries deletes the given cached keys and recomputes them in the background.
func (m *DiscoveryManager) RerunEntries(keys []string) error {
	if m.store == nil || len(keys) == 0 {
		return nil
	}
	tasks := make([]discoveryTask, 0, len(keys))
	for _, k := range keys {
		e, ok, err := m.store.GetCorrelationCacheEntry(k)
		if err != nil || !ok {
			continue
		}
		var req CorrelationRequest
		if err := json.Unmarshal(e.RequestJSON, &req); err != nil {
			continue
		}
		tasks = append(tasks, discoveryTask{key: k, req: req})
	}
	if len(tasks) == 0 {
		return nil
	}

	m.stopWorkerLocked()
	_, _ = m.store.DeleteCorrelationCacheEntries(keys)

	now := time.Now().UTC()
	m.mu.Lock()
	cfg := m.progress.Config
	cfg.ForceRerun = true
	m.tasks = tasks
	m.progress = DiscoveryProgress{
		Status:    "running",
		Config:    cfg,
		Total:     len(tasks),
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

func (m *DiscoveryManager) stopWorkerLocked() {
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

func (m *DiscoveryManager) launchWorkerLocked(startCursor int) {
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	m.cancel = cancel
	m.doneCh = doneCh

	tasks := m.tasks
	cfg := m.progress.Config
	go m.runWorker(ctx, doneCh, tasks, cfg, startCursor)
}

func (m *DiscoveryManager) runWorker(ctx context.Context, doneCh chan struct{}, tasks []discoveryTask, cfg DiscoveryConfig, startCursor int) {
	defer close(doneCh)

	seriesCache := make(map[string][]RawSeriesPoint)
	granularity := resolveGranularity(cfg.RangeKey, cfg.Granularity)
	now := time.Now().UTC()
	fromTS := rangeToFromTS(cfg.RangeKey, now)
	toTS := now.Format("200601021504")

	loadCachedSeries := func(spec SeriesSpec) ([]RawSeriesPoint, error) {
		sk := fmt.Sprintf("%s:%s:%s:%s:%s", spec.Kind, spec.LocationID, spec.Metric, cfg.RangeKey, granularity)
		if pts, ok := seriesCache[sk]; ok {
			return pts, nil
		}
		prov, ok := GetProvider(spec.Kind)
		if !ok {
			return nil, fmt.Errorf("unsupported series kind: %s", spec.Kind)
		}
		pts, err := prov.LoadSeries(m.store, spec.LocationID, spec.Metric, fromTS, toTS, granularity)
		if err != nil {
			seriesCache[sk] = nil
			return nil, err
		}
		seriesCache[sk] = pts
		return pts, nil
	}

	lastEmit := time.Now()
	for i := startCursor; i < len(tasks); i++ {
		if ctx.Err() != nil {
			m.mu.Lock()
			m.progress.Status = "paused"
			m.progress.Completed = i
			m.progress.CurrentPair = ""
			m.progress.CurrentAlgorithm = ""
			m.progress.UpdatedAt = time.Now().UTC()
			m.persistStateLocked()
			prog := m.progress
			m.mu.Unlock()
			m.emitProgress(prog)
			return
		}

		task := tasks[i]
		pairLabel := fmt.Sprintf("%s/%s × %s/%s",
			task.req.SeriesX.LocationID, task.req.SeriesX.Metric,
			task.req.SeriesY.LocationID, task.req.SeriesY.Metric)

		if !cfg.ForceRerun {
			if _, ok, err := m.store.GetCorrelationCacheEntry(task.key); err == nil && ok {
				m.mu.Lock()
				m.progress.Completed = i + 1
				m.progress.CacheHits++
				m.progress.CurrentPair = pairLabel
				m.progress.CurrentAlgorithm = task.req.Algorithm
				m.progress.UpdatedAt = time.Now().UTC()
				prog := m.progress
				m.mu.Unlock()
				if time.Since(lastEmit) >= 120*time.Millisecond || i == len(tasks)-1 {
					m.emitProgress(prog)
					lastEmit = time.Now()
				}
				continue
			}
		}

		ptsX, errX := loadCachedSeries(task.req.SeriesX)
		ptsY, errY := loadCachedSeries(task.req.SeriesY)
		if errX != nil || errY != nil || len(ptsX) < cfg.MinSamples || len(ptsY) < cfg.MinSamples {
			m.mu.Lock()
			m.progress.Completed = i + 1
			m.progress.Skipped++
			if errX != nil || errY != nil {
				m.progress.Errors++
			}
			m.progress.CurrentPair = pairLabel
			m.progress.CurrentAlgorithm = task.req.Algorithm
			m.progress.UpdatedAt = time.Now().UTC()
			prog := m.progress
			m.mu.Unlock()
			if time.Since(lastEmit) >= 120*time.Millisecond || i == len(tasks)-1 {
				m.emitProgress(prog)
				lastEmit = time.Now()
			}
			continue
		}

		pairs, warnings, err := AlignAndPreprocess(ptsX, ptsY, granularity, task.req.Preprocess, task.req.Algorithm != "cross_correlation")
		if err != nil || len(pairs) < cfg.MinSamples {
			m.mu.Lock()
			m.progress.Completed = i + 1
			m.progress.Skipped++
			m.progress.UpdatedAt = time.Now().UTC()
			prog := m.progress
			m.mu.Unlock()
			if time.Since(lastEmit) >= 120*time.Millisecond || i == len(tasks)-1 {
				m.emitProgress(prog)
				lastEmit = time.Now()
			}
			continue
		}

		res, err := RunStatistical(task.req, pairs, granularity, warnings)
		if err != nil {
			m.mu.Lock()
			m.progress.Completed = i + 1
			m.progress.Errors++
			m.progress.LastError = err.Error()
			m.progress.UpdatedAt = time.Now().UTC()
			prog := m.progress
			m.mu.Unlock()
			if time.Since(lastEmit) >= 120*time.Millisecond || i == len(tasks)-1 {
				m.emitProgress(prog)
				lastEmit = time.Now()
			}
			continue
		}

		score := computeDiscoveryScore(res)
		reqJSON, _ := json.Marshal(task.req)
		metricsJSON, _ := json.Marshal(res.Metrics)
		nowItem := time.Now().UTC()
		entry := store.CorrelationCacheEntry{
			Key:         task.key,
			RequestJSON: reqJSON,
			MetricsJSON: metricsJSON,
			Algorithm:   res.Algorithm,
			Granularity: res.Granularity,
			SampleCount: res.SampleCount,
			Score:       score,
			Warnings:    res.Warnings,
			UpdatedAt:   nowItem,
		}
		_ = m.store.SaveCorrelationCacheEntry(entry)

		item := DiscoveryResultItem{
			Key:         task.key,
			Request:     task.req,
			Metrics:     res.Metrics,
			SampleCount: res.SampleCount,
			Score:       score,
			Warnings:    res.Warnings,
			UpdatedAt:   nowItem,
		}
		if m.onResult != nil {
			m.onResult(item)
		}

		m.mu.Lock()
		m.progress.Completed = i + 1
		m.progress.Computed++
		m.progress.CurrentPair = pairLabel
		m.progress.CurrentAlgorithm = task.req.Algorithm
		m.progress.UpdatedAt = nowItem
		if (i+1)%25 == 0 {
			m.persistStateLocked()
		}
		prog := m.progress
		m.mu.Unlock()

		if time.Since(lastEmit) >= 120*time.Millisecond || i == len(tasks)-1 {
			m.emitProgress(prog)
			lastEmit = time.Now()
		}
	}

	m.mu.Lock()
	m.progress.Status = "completed"
	m.progress.Completed = len(tasks)
	m.progress.CurrentPair = ""
	m.progress.CurrentAlgorithm = ""
	m.progress.UpdatedAt = time.Now().UTC()
	m.persistStateLocked()
	prog := m.progress
	m.mu.Unlock()
	m.emitProgress(prog)
}

func (m *DiscoveryManager) persistStateLocked() {
	if m.store == nil {
		return
	}
	cfgJSON, _ := json.Marshal(m.progress.Config)
	_ = m.store.SaveDiscoveryState(store.DiscoverySavedState{
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

func (m *DiscoveryManager) emitProgress(prog DiscoveryProgress) {
	if m.onProgress != nil {
		m.onProgress(prog)
	}
}

func computeDiscoveryScore(res CorrelationResult) float64 {
	if res.Algorithm == "cross_correlation" {
		if math.IsNaN(res.Metrics.BestLagCorrelation) {
			return 0
		}
		return math.Abs(res.Metrics.BestLagCorrelation)
	}
	if math.IsNaN(res.Metrics.Correlation) {
		return 0
	}
	return math.Abs(res.Metrics.Correlation)
}

func entryToItem(e store.CorrelationCacheEntry) (DiscoveryResultItem, error) {
	var req CorrelationRequest
	if err := json.Unmarshal(e.RequestJSON, &req); err != nil {
		return DiscoveryResultItem{}, err
	}
	var metrics CorrelationMetrics
	if err := json.Unmarshal(e.MetricsJSON, &metrics); err != nil {
		return DiscoveryResultItem{}, err
	}
	return DiscoveryResultItem{
		Key:         e.Key,
		Request:     req,
		Metrics:     metrics,
		SampleCount: e.SampleCount,
		Score:       e.Score,
		Warnings:    e.Warnings,
		UpdatedAt:   e.UpdatedAt,
	}, nil
}

func buildDiscoveryTasks(catalog SeriesCatalog, cfg DiscoveryConfig) []discoveryTask {
	kindFilter := toSet(cfg.Kinds)
	locFilter := toSet(cfg.LocationIDs)
	metricFilter := toSet(cfg.Metrics)

	metricsByKind := make(map[string][]CatalogMetric)
	for _, m := range catalog.Metrics {
		if len(metricFilter) > 0 && !metricFilter[m.ID] {
			continue
		}
		metricsByKind[m.Kind] = append(metricsByKind[m.Kind], m)
	}

	var seriesList []SeriesSpec
	for _, loc := range catalog.Locations {
		if len(kindFilter) > 0 && !kindFilter[loc.Kind] {
			continue
		}
		if len(locFilter) > 0 && !locFilter[loc.ID] {
			continue
		}
		label := loc.Name
		if label == "" {
			label = loc.NameEn
		}
		for _, m := range metricsByKind[loc.Kind] {
			seriesList = append(seriesList, SeriesSpec{
				Kind:       loc.Kind,
				LocationID: loc.ID,
				Metric:     m.ID,
				Label:      label,
			})
		}
	}

	sort.Slice(seriesList, func(i, j int) bool {
		ki := seriesIdentity(seriesList[i])
		kj := seriesIdentity(seriesList[j])
		return ki < kj
	})

	var tasks []discoveryTask
	for i := 0; i < len(seriesList); i++ {
		for j := i + 1; j < len(seriesList); j++ {
			sx := seriesList[i]
			sy := seriesList[j]
			if cfg.CrossKindOnly && sx.Kind == sy.Kind {
				continue
			}
			if !cfg.SameLocationAllowed && sx.Kind == sy.Kind && sx.LocationID == sy.LocationID {
				continue
			}
			for _, deseason := range cfg.DeseasonalizeModes {
				for _, alg := range cfg.Algorithms {
					req := CorrelationRequest{
						SeriesX:     sx,
						SeriesY:     sy,
						RangeKey:    cfg.RangeKey,
						Granularity: cfg.Granularity,
						Preprocess: PreprocessOptions{
							Deseasonalize:   deseason,
							MovingAvgWindow: cfg.MovingAvgWindow,
							Difference:      cfg.Difference,
						},
						Algorithm: alg,
						Params: AlgorithmParams{
							MaxLag: cfg.MaxLag,
						},
					}
					tasks = append(tasks, discoveryTask{
						key: CorrelationRequestKey(req),
						req: req,
					})
				}
			}
		}
	}
	return tasks
}

func seriesIdentity(s SeriesSpec) string {
	return strings.Join([]string{s.Kind, s.LocationID, s.Metric}, ":")
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]bool, len(items))
	for _, it := range items {
		if it != "" {
			out[it] = true
		}
	}
	return out
}
