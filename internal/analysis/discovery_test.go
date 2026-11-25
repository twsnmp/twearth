package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

func seedTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := store.DefaultConfig()
	cfg.Locations = cfg.Locations[:1]
	cfg.MarineLocations = cfg.MarineLocations[:1]
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	locID := cfg.Locations[0].ID
	marID := cfg.MarineLocations[0].ID

	start := time.Now().UTC().AddDate(-2, 0, 0)
	var wPts []store.DailyWeatherPoint
	var mPts []store.DailyMarinePoint
	for i := 0; i < 600; i++ {
		day := start.AddDate(0, 0, i).Format("20060102")
		sst := 22.0 + 3.0*math.Sin(float64(i)*0.05)
		temp := 15.0 + 2.0*sst + 0.5*math.Cos(float64(i)*0.02)
		wPts = append(wPts, store.DailyWeatherPoint{
			LocationID:      locID,
			Date:            day,
			TempMean:        temp,
			PressureMSLMean: 1013.0 - 0.8*sst,
		})
		mPts = append(mPts, store.DailyMarinePoint{
			LocationID: marID,
			Date:       day,
			SSTMean:    sst,
		})
	}
	if err := st.SaveDailyWeatherBatch(wPts); err != nil {
		t.Fatalf("save daily weather: %v", err)
	}
	if err := st.SaveDailyMarineBatch(mPts); err != nil {
		t.Fatalf("save daily marine: %v", err)
	}
	return st
}

func TestBuildDiscoveryTasksDeduplicatesSymmetricPairs(t *testing.T) {
	cat := SeriesCatalog{
		Kinds: []CatalogKind{
			{ID: "weather", DefaultMetric: "temperature2m"},
			{ID: "marine", DefaultMetric: "seaSurfaceTemperature"},
		},
		Locations: []CatalogLocation{
			{ID: "tokyo", Kind: "weather", Name: "Tokyo"},
			{ID: "nino3", Kind: "marine", Name: "Nino3"},
		},
		Metrics: []CatalogMetric{
			{ID: "temperature2m", Kind: "weather"},
			{ID: "seaSurfaceTemperature", Kind: "marine"},
		},
	}

	tasks := buildDiscoveryTasks(cat, DiscoveryConfig{
		RangeKey:            "5y",
		Granularity:         "monthly",
		Algorithms:          []string{"pearson", "spearman"},
		DeseasonalizeModes:  []bool{true},
		SameLocationAllowed: true,
	})
	if len(tasks) != 2 {
		t.Fatalf("expected 2 unique tasks (1 pair * 2 algs), got %d", len(tasks))
	}
}

func TestDiscoveryManagerStartCacheAndRerun(t *testing.T) {
	st := seedTestStore(t)

	doneCh := make(chan DiscoveryProgress, 8)
	dm := NewDiscoveryManager(st, func(p DiscoveryProgress) {
		if p.Status == "completed" {
			select {
			case doneCh <- p:
			default:
			}
		}
	}, nil)

	err := dm.Start(DiscoveryConfig{
		RangeKey:           "5y",
		Granularity:        "monthly",
		Algorithms:         []string{"pearson"},
		DeseasonalizeModes: []bool{false},
		Metrics:            []string{"temperature2m", "seaSurfaceTemperature"},
		MinSamples:         6,
		CrossKindOnly:      true,
	})
	if err != nil {
		t.Fatalf("start discovery: %v", err)
	}

	select {
	case prog := <-doneCh:
		if prog.Computed != 1 || prog.Total != 1 {
			t.Fatalf("expected 1 computed out of 1, got %+v", prog)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for discovery completion")
	}

	cached, err := dm.ListCached()
	if err != nil || len(cached) != 1 {
		t.Fatalf("expected 1 cached item, got %d (err=%v)", len(cached), err)
	}
	if cached[0].Score < 0.9 {
		t.Fatalf("expected strong synthetic correlation > 0.9, got %f", cached[0].Score)
	}

	// Second run should hit cache.
	if err := dm.Start(DiscoveryConfig{
		RangeKey:           "5y",
		Granularity:        "monthly",
		Algorithms:         []string{"pearson"},
		DeseasonalizeModes: []bool{false},
		Metrics:            []string{"temperature2m", "seaSurfaceTemperature"},
		MinSamples:         6,
		CrossKindOnly:      true,
	}); err != nil {
		t.Fatalf("second start: %v", err)
	}
	select {
	case prog := <-doneCh:
		if prog.CacheHits != 1 || prog.Computed != 0 {
			t.Fatalf("expected 1 cache hit and 0 computed, got %+v", prog)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for cached run")
	}

	// Rerun single entry should recompute it.
	if err := dm.RerunEntries([]string{cached[0].Key}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	select {
	case prog := <-doneCh:
		if prog.Computed != 1 {
			t.Fatalf("expected 1 recomputed, got %+v", prog)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for rerun")
	}
}
