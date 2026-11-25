package analysis

import (
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

type customTestFactor struct{}

func (customTestFactor) Descriptor() RankingFactorDescriptor {
	return RankingFactorDescriptor{
		ID:            "future_air_quality",
		DefaultWeight: 15,
	}
}

func (customTestFactor) Evaluate(_ *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore {
	score := 90.0
	if muni.Group == "global" {
		score = 60.0
	}
	return RankingFactorScore{
		FactorID:   "future_air_quality",
		Score:      score,
		Applicable: true,
		Summary:    "PM2.5 test factor",
		Metrics:    map[string]float64{"pm25": 8.5},
	}
}

func TestBuiltinMunicipalitiesCatalog(t *testing.T) {
	all := store.BuiltinMunicipalities()
	if len(all) < 1900 {
		t.Fatalf("expected at least 1900 built-in municipalities (Japan + Global), got %d", len(all))
	}
	var jpCount, globalCount int
	prefSet := make(map[string]bool)
	for _, m := range all {
		if m.ID == "" || m.Name == "" || (m.Latitude == 0 && m.Longitude == 0) {
			t.Fatalf("invalid municipality entry: %+v", m)
		}
		if m.Group == "japan" {
			jpCount++
			prefSet[m.Prefecture] = true
		} else if m.Group == "global" {
			globalCount++
		}
	}
	if jpCount < 1800 {
		t.Fatalf("expected at least 1800 Japanese municipalities, got %d", jpCount)
	}
	if len(prefSet) != 47 {
		t.Fatalf("expected all 47 Japanese prefectures, got %d", len(prefSet))
	}
	if globalCount < 100 {
		t.Fatalf("expected at least 100 major global cities, got %d", globalCount)
	}
}

func TestRankingManagerAndExtensibility(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Seed custom municipality
	added, err := st.SaveCustomMunicipality(store.MunicipalityEntry{
		Name:       "テスト安心市",
		NameEn:     "Test Safe City",
		Prefecture: "岡山県",
		Group:      "japan",
		Region:     "chugoku",
		Latitude:   34.66,
		Longitude:  133.93,
	})
	if err != nil {
		t.Fatalf("SaveCustomMunicipality: %v", err)
	}
	if added.ID == "" {
		t.Fatalf("expected generated ID for custom municipality")
	}

	// Seed earthquakes & disasters near a specific coordinate (e.g. 35.68, 139.75 Tokyo)
	now := time.Now().UTC()
	_ = st.SaveQuakeBatch([]store.QuakeRecord{
		{
			ID:         "q1",
			Source:     "p2pquake",
			Hypocenter: "東京湾",
			Magnitude:  6.2,
			MaxScale:   "5強",
			Latitude:   35.65,
			Longitude:  139.78,
			OccurredAt: now.Add(-10 * 24 * time.Hour),
		},
	})
	_ = st.SaveDisasterBatch([]store.DisasterRecord{
		{
			ID:        "d1",
			Source:    "jma_extra",
			Category:  "flood",
			Severity:  "critical",
			Title:     "大雨特別警報",
			Latitude:  35.68,
			Longitude: 139.75,
			StartedAt: now.Add(-5 * 24 * time.Hour),
			UpdatedAt: now.Add(-5 * 24 * time.Hour),
		},
	})

	// Register a future factor provider to verify plug-and-play extensibility
	RegisterRankingFactor(customTestFactor{})
	defer UnregisterRankingFactor("future_air_quality")

	cat := BuildRankingCatalog(st)
	foundCustomFactor := false
	for _, f := range cat.Factors {
		if f.ID == "future_air_quality" {
			foundCustomFactor = true
			break
		}
	}
	if !foundCustomFactor {
		t.Fatalf("expected newly registered factor 'future_air_quality' in RankingCatalog")
	}
	if cat.TotalCustomCount != 1 {
		t.Fatalf("expected TotalCustomCount=1, got %d", cat.TotalCustomCount)
	}

	doneCh := make(chan struct{}, 1)
	mgr := NewRankingManager(st, nil, nil, func(p RankingProgress) {
		if p.Status == "completed" {
			select {
			case doneCh <- struct{}{}:
			default:
			}
		}
	}, nil)

	cfg := defaultRankingConfig()
	cfg.Scope = "japan_pref"
	cfg.Prefecture = "岡山県"
	if err := mgr.Start(cfg); err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for ranking completion")
	}

	results, err := mgr.ListResults()
	if err != nil {
		t.Fatalf("ListResults: %v", err)
	}
	if len(results) < 25 {
		t.Fatalf("expected at least 25 Okayama municipalities (including custom), got %d", len(results))
	}
	if results[0].Rank != 1 {
		t.Fatalf("expected top item to have Rank=1, got %d", results[0].Rank)
	}
	if _, ok := results[0].FactorScores["future_air_quality"]; !ok {
		t.Fatalf("expected 'future_air_quality' factor score in evaluated item")
	}
}
