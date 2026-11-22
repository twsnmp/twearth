package store

import (
	"encoding/json"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func hasMarineID(list []MarineLocation, id string) bool {
	for _, m := range list {
		if m.ID == id {
			return true
		}
	}
	return false
}

func TestRecommendMarineLocationsJapanCities(t *testing.T) {
	cases := []struct {
		name           string
		loc            ObservationLocation
		expectPrimary  string
		expectIncludes []string
	}{
		{
			name:           "Tokyo",
			loc:            ObservationLocation{ID: "tokyo", Name: "東京", Latitude: 35.6762, Longitude: 139.6503},
			expectPrimary:  "tokyo-bay",
			expectIncludes: []string{"sagami-bay", "kuroshio-shionomisaki", "oyashio-sanriku", "sea-of-japan-central", "east-china-sea", "nino-3", "nino-west", "iobw"},
		},
		{
			name:           "Sapporo",
			loc:            ObservationLocation{ID: "sapporo", Name: "札幌", Latitude: 43.0618, Longitude: 141.3545},
			expectPrimary:  "ishikari-bay",
			expectIncludes: []string{"oyashio-kushiro", "kuroshio-shionomisaki", "oyashio-sanriku", "sea-of-japan-central", "east-china-sea", "nino-3", "nino-west", "iobw"},
		},
		{
			name:           "Fukuoka",
			loc:            ObservationLocation{ID: "fukuoka", Name: "福岡", Latitude: 33.5904, Longitude: 130.4017},
			expectPrimary:  "tsushima-strait",
			expectIncludes: []string{"hyuga-nada", "kuroshio-shionomisaki", "oyashio-sanriku", "sea-of-japan-central", "east-china-sea", "nino-3", "nino-west", "iobw"},
		},
		{
			name:           "Naha",
			loc:            ObservationLocation{ID: "naha", Name: "那覇", Latitude: 26.2124, Longitude: 127.6809},
			expectPrimary:  "okinawa-offshore",
			expectIncludes: []string{"east-china-sea", "kuroshio-shionomisaki", "oyashio-sanriku", "sea-of-japan-central", "nino-3", "nino-west", "iobw"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := RecommendMarineLocations(tc.loc, true)
			if len(recs) == 0 {
				t.Fatalf("expected non-empty recommendations")
			}
			if recs[0].ID != tc.expectPrimary || !recs[0].IsPrimary {
				t.Fatalf("expected primary %s, got %+v", tc.expectPrimary, recs[0])
			}
			for _, id := range tc.expectIncludes {
				if !hasMarineID(recs, id) {
					t.Fatalf("expected %s in recommendations for %s, got %+v", id, tc.name, recs)
				}
			}
		})
	}

	noClimate := RecommendMarineLocations(ObservationLocation{ID: "tokyo", Latitude: 35.6762, Longitude: 139.6503}, false)
	for _, cid := range []string{"nino-3", "nino-west", "iobw"} {
		if hasMarineID(noClimate, cid) {
			t.Fatalf("did not expect climate ID %s when includeClimate=false", cid)
		}
	}
}

func TestRecommendMarineLocationsGlobalCities(t *testing.T) {
	london := ObservationLocation{ID: "london", Name: "London", Latitude: 51.5072, Longitude: -0.1276}
	recs := RecommendMarineLocations(london, true)
	if len(recs) == 0 || recs[0].ID != "english-channel" || !recs[0].IsPrimary {
		t.Fatalf("expected english-channel primary for London, got %+v", recs)
	}
	if !hasMarineID(recs, "north-sea") || !hasMarineID(recs, "nino-3") {
		t.Fatalf("expected north-sea and nino-3 for London, got %+v", recs)
	}

	sydney := ObservationLocation{ID: "sydney", Name: "Sydney", Latitude: -33.8688, Longitude: 151.2093}
	sRecs := RecommendMarineLocations(sydney, false)
	if len(sRecs) == 0 || sRecs[0].ID != "tasman-sea-sydney" {
		t.Fatalf("expected tasman-sea-sydney primary for Sydney, got %+v", sRecs)
	}
}

func TestMergeAutoMarineLocationsPreservesManualAndUpdatesOnPrimarySwitch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cfg, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}

	// Add a manual custom point and remove one auto point without changing primary city.
	cfg.MarineLocations = append(cfg.MarineLocations, MarineLocation{
		ID:        "custom-point",
		Name:      "カスタム海域",
		Latitude:  31.0,
		Longitude: 131.0,
		Source:    "custom",
		Category:  "regional",
	})
	var filtered []MarineLocation
	for _, m := range cfg.MarineLocations {
		if m.ID != "sagami-bay" {
			filtered = append(filtered, m)
		}
	}
	cfg.MarineLocations = filtered
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig same primary: %v", err)
	}

	loaded, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after same primary: %v", err)
	}
	if !hasMarineID(loaded.MarineLocations, "custom-point") {
		t.Fatalf("expected custom-point to be preserved")
	}
	if hasMarineID(loaded.MarineLocations, "sagami-bay") {
		t.Fatalf("expected manual removal of sagami-bay to persist while primary stays unchanged")
	}

	// Switch primary city to Sapporo -> auto entries update, custom-point survives.
	loaded.Locations = []ObservationLocation{
		{ID: "tokyo", Name: "東京", Latitude: 35.6762, Longitude: 139.6503, Source: "preset", IsPrimary: false},
		{ID: "sapporo", Name: "札幌", Latitude: 43.0618, Longitude: 141.3545, Source: "preset", IsPrimary: true},
	}
	if err := s.SaveConfig(loaded); err != nil {
		t.Fatalf("SaveConfig Sapporo primary: %v", err)
	}

	afterSwitch, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after switch: %v", err)
	}
	if !hasMarineID(afterSwitch.MarineLocations, "ishikari-bay") {
		t.Fatalf("expected ishikari-bay after switching primary to Sapporo, got %+v", afterSwitch.MarineLocations)
	}
	if !hasMarineID(afterSwitch.MarineLocations, "custom-point") {
		t.Fatalf("expected custom-point to survive primary city switch")
	}
	if afterSwitch.PrimaryMarineLocation().ID != "ishikari-bay" {
		t.Fatalf("expected ishikari-bay to be primary marine location, got %+v", afterSwitch.PrimaryMarineLocation())
	}
}

func TestStoreOpenMigratesLegacyMarineConfigAndClearsOldHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}

	// Simulate legacy v0 config (only old 2 marine points, no marineConfigVersion) and legacy marine data.
	legacyCfg, _ := json.Marshal(map[string]any{
		"language": "ja",
		"locations": []map[string]any{
			{
				"id":        "fukuoka",
				"name":      "福岡",
				"latitude":  33.5904,
				"longitude": 130.4017,
				"source":    "preset",
				"isPrimary": true,
			},
		},
		"marineLocations": []map[string]any{
			{"id": "sagami-bay", "name": "相模湾", "latitude": 35.15, "longitude": 139.35, "isPrimary": true},
			{"id": "kuroshio-shionomisaki", "name": "潮岬沖", "latitude": 33.25, "longitude": 135.75, "isPrimary": false},
		},
	})
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketConfig).Put(configKey, legacyCfg)
	}); err != nil {
		t.Fatalf("put legacy config: %v", err)
	}
	if err := s.SaveMarineTimeSeries(MarineTimeSeriesPoint{
		LocationID:            "sagami-bay",
		Timestamp:             "202611010000",
		SeaSurfaceTemperature: 21.0,
	}); err != nil {
		t.Fatalf("SaveMarineTimeSeries: %v", err)
	}
	if err := s.SaveDailyMarineBatch([]DailyMarinePoint{
		{LocationID: "sagami-bay", Date: "20261101", SSTMean: 21.0},
	}); err != nil {
		t.Fatalf("SaveDailyMarineBatch: %v", err)
	}
	if err := s.SaveHistorySyncState(HistorySyncState{Kind: "marine", LocationID: "sagami-bay", BackfillDone: true}); err != nil {
		t.Fatalf("SaveHistorySyncState marine: %v", err)
	}
	if err := s.SaveHistorySyncState(HistorySyncState{Kind: "weather", LocationID: "fukuoka", BackfillDone: true}); err != nil {
		t.Fatalf("SaveHistorySyncState weather: %v", err)
	}
	_ = s.Close()

	// Re-open triggers one-time legacy marine cleanup & primary-based auto selection.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	defer func() { _ = s2.Close() }()

	cfg, err := s2.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after migration: %v", err)
	}
	if !cfg.AutoMarineByPrimary || !cfg.AutoMarineClimate || cfg.MarineConfigVersion != 1 {
		t.Fatalf("unexpected migrated flags: %+v", cfg)
	}
	if cfg.PrimaryMarineLocation().ID != "tsushima-strait" {
		t.Fatalf("expected tsushima-strait primary for Fukuoka after migration, got %+v", cfg.PrimaryMarineLocation())
	}
	if !hasMarineID(cfg.MarineLocations, "nino-3") || !hasMarineID(cfg.MarineLocations, "nino-west") || !hasMarineID(cfg.MarineLocations, "iobw") {
		t.Fatalf("expected El Nino regions after migration, got %+v", cfg.MarineLocations)
	}

	_, _, mCount, _ := s2.GetTimeSeriesBounds("marine", "sagami-bay")
	_, _, dCount, _ := s2.GetDailyBounds("marine", "sagami-bay")
	if mCount != 0 || dCount != 0 {
		t.Fatalf("expected old marine data cleared on migration, got hourly=%d daily=%d", mCount, dCount)
	}
	if _, hasMarineSync, _ := s2.GetHistorySyncState("marine", "sagami-bay"); hasMarineSync {
		t.Fatalf("expected old marine sync state to be cleared")
	}
	if _, hasWeatherSync, _ := s2.GetHistorySyncState("weather", "fukuoka"); !hasWeatherSync {
		t.Fatalf("expected non-marine weather sync state to be preserved")
	}
}
