package store

import (
	"testing"
	"time"
)

func TestDisasterAndLoreStorageAndAnalysis(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 11, 22, 5, 0, 0, 0, time.UTC)
	events := []DisasterRecord{
		{
			ID:        "jma-1",
			Source:    "jma_extra",
			Category:  "landslide",
			Severity:  "critical",
			Title:     "土砂災害警戒情報 東京都",
			Latitude:  35.68,
			Longitude: 139.65,
			StartedAt: now.Add(-2 * time.Hour),
			UpdatedAt: now.Add(-1 * time.Hour),
			IsActive:  true,
		},
		{
			ID:                "gdacs-1",
			Source:            "gdacs",
			Category:          "flood",
			Severity:          "warning",
			AlertLevel:        "Orange",
			Title:             "Flood in Southeast Asia",
			Latitude:          13.75,
			Longitude:         100.50,
			PopulationExposed: 150000,
			StartedAt:         now.Add(-26 * time.Hour),
			UpdatedAt:         now.Add(-25 * time.Hour),
			IsActive:          true,
		},
		{
			ID:        "eonet-1",
			Source:    "eonet",
			Category:  "wildfire",
			Severity:  "info",
			Title:     "Wildfire California",
			Latitude:  34.05,
			Longitude: -118.24,
			StartedAt: now.Add(-48 * time.Hour),
			UpdatedAt: now.Add(-48 * time.Hour),
			IsActive:  false,
		},
	}

	if err := st.SaveDisasterBatch(events); err != nil {
		t.Fatalf("SaveDisasterBatch: %v", err)
	}

	// Upsert with updated timestamp should not duplicate index entries.
	events[0].UpdatedAt = now
	if err := st.SaveDisasterBatch(events[:1]); err != nil {
		t.Fatalf("SaveDisasterBatch update: %v", err)
	}

	recent, err := st.ListRecentDisasters(10, false)
	if err != nil {
		t.Fatalf("ListRecentDisasters: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent disasters, got %d", len(recent))
	}
	if recent[0].ID != "jma-1" {
		t.Fatalf("expected newest event jma-1 first, got %s", recent[0].ID)
	}

	activeOnly, err := st.ListRecentDisasters(10, true)
	if err != nil {
		t.Fatalf("ListRecentDisasters activeOnly: %v", err)
	}
	if len(activeOnly) != 2 {
		t.Fatalf("expected 2 active disasters, got %d", len(activeOnly))
	}

	// Save lore near Tokyo and far away.
	lores := []DisasterLoreRecord{
		{
			ID:           "lore-1",
			Name:         "洪水記念碑",
			DisasterKind: "洪水",
			Category:     "flood",
			Year:         1910,
			Latitude:     35.70,
			Longitude:    139.70,
		},
		{
			ID:           "lore-2",
			Name:         "津波碑",
			DisasterKind: "津波",
			Category:     "tsunami",
			Year:         1933,
			Latitude:     39.27,
			Longitude:    141.88,
		},
	}
	if err := st.SaveDisasterLoreBatch(lores); err != nil {
		t.Fatalf("SaveDisasterLoreBatch: %v", err)
	}
	cnt, err := st.CountDisasterLore()
	if err != nil || cnt != 2 {
		t.Fatalf("CountDisasterLore = %d, %v", cnt, err)
	}

	// Query disaster history near Tokyo (radius 50km).
	res, err := st.QueryDisasterHistory(DisasterHistoryQuery{
		Latitude:  35.68,
		Longitude: 139.65,
		RadiusKm:  50,
		FromTime:  now.Add(-72 * time.Hour),
		ToTime:    now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("QueryDisasterHistory: %v", err)
	}
	if res.TotalCount != 1 || res.CriticalCount != 1 {
		t.Fatalf("unexpected spatial query result: %+v", res)
	}
	if res.NearbyLore.TotalCount != 1 || res.NearbyLore.EarliestYear != 1910 {
		t.Fatalf("unexpected nearby lore summary: %+v", res.NearbyLore)
	}
}

func TestRiverTimeSeriesStorageAndQuery(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	t0 := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	pts := []RiverTimeSeriesPoint{
		{
			StationID:     "st-100",
			StationName:   "荒川・岩淵水門",
			RiverName:     "荒川",
			Timestamp:     t0.Format("200601021504"),
			ObservedAt:    t0,
			Latitude:      35.78,
			Longitude:     139.73,
			WaterLevelM:   2.1,
			WarningLevelM: 4.5,
			DangerLevelM:  6.0,
			Status:        "normal",
		},
		{
			StationID:     "st-100",
			StationName:   "荒川・岩淵水門",
			RiverName:     "荒川",
			Timestamp:     t0.Add(time.Hour).Format("200601021504"),
			ObservedAt:    t0.Add(time.Hour),
			Latitude:      35.78,
			Longitude:     139.73,
			WaterLevelM:   4.8,
			WarningLevelM: 4.5,
			DangerLevelM:  6.0,
			Status:        "warning",
		},
	}
	if err := st.SaveRiverTimeSeriesBatch(pts); err != nil {
		t.Fatalf("SaveRiverTimeSeriesBatch: %v", err)
	}

	latest, err := st.ListLatestRiverStations()
	if err != nil {
		t.Fatalf("ListLatestRiverStations: %v", err)
	}
	if len(latest) != 1 || latest[0].WaterLevelM != 4.8 {
		t.Fatalf("unexpected latest stations: %+v", latest)
	}

	hist, err := st.QueryRiverHistory(RiverHistoryQuery{StationID: "st-100"})
	if err != nil {
		t.Fatalf("QueryRiverHistory: %v", err)
	}
	if len(hist.Points) != 2 || hist.MaxLevelM != 4.8 || hist.MinLevelM != 2.1 {
		t.Fatalf("unexpected river history: %+v", hist)
	}
}
