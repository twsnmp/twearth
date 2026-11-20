package store

import (
	"encoding/json"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestStoreConfigTimeSeriesAndEvents(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = s.Close() }()

	cfg, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if len(cfg.Locations) == 0 || cfg.PrimaryLocation().Latitude == 0 {
		t.Fatalf("expected default observation locations, got %+v", cfg.Locations)
	}
	if len(cfg.MarineLocations) == 0 || cfg.PrimaryMarineLocation().Latitude == 0 {
		t.Fatalf("expected default marine locations, got %+v", cfg.MarineLocations)
	}
	if cfg.RainAlertMm <= 0 || cfg.SnowfallAlertCm <= 0 {
		t.Fatalf("expected default precip alert thresholds, got rain=%.1f snow=%.1f", cfg.RainAlertMm, cfg.SnowfallAlertCm)
	}

	cfg.Language = "en"
	cfg.Locations = append(cfg.Locations, ObservationLocation{
		ID:        "osaka",
		Name:      "大阪",
		NameEn:    "Osaka",
		Latitude:  34.6937,
		Longitude: 135.5023,
		Source:    "preset",
	})
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after save failed: %v", err)
	}
	if loaded.Language != "en" || len(loaded.Locations) != 2 || loaded.Locations[1].ID != "osaka" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}

	if err := s.SaveTimeSeries(TimeSeriesPoint{
		Timestamp:     "202611190500",
		PressureMSL:   1013.2,
		Precipitation: 4.5,
		Rain:          4.5,
		Snowfall:      1.2,
		SnowDepth:     0.15,
		KpIndex:       2.3,
	}); err != nil {
		t.Fatalf("SaveTimeSeries failed: %v", err)
	}

	pts, err := s.ListTimeSeries("202611190000", "202611192359")
	if err != nil {
		t.Fatalf("ListTimeSeries failed: %v", err)
	}
	if len(pts) != 1 || pts[0].PressureMSL != 1013.2 || pts[0].Precipitation != 4.5 || pts[0].Snowfall != 1.2 || pts[0].SnowDepth != 0.15 {
		t.Fatalf("unexpected timeseries points: %+v", pts)
	}

	base := time.Date(2026, 11, 19, 5, 0, 0, 0, time.UTC)
	if err := s.SaveEvent(EventRecord{
		ID:        "ev1",
		Type:      "TWEARTH-QUAKE",
		Severity:  "info",
		Title:     "M4.2",
		Timestamp: base,
	}); err != nil {
		t.Fatalf("SaveEvent 1 failed: %v", err)
	}
	if err := s.SaveEvent(EventRecord{
		ID:        "ev2",
		Type:      "TWEARTH-SPACE",
		Severity:  "warning",
		Title:     "Kp 5.5",
		Timestamp: base.Add(time.Minute),
	}); err != nil {
		t.Fatalf("SaveEvent 2 failed: %v", err)
	}

	evs, err := s.ListEvents(10)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(evs) != 2 || evs[0].ID != "ev2" || evs[1].ID != "ev1" {
		t.Fatalf("expected newest-first events [ev2, ev1], got %+v", evs)
	}
}

func TestStoreMigratesLegacyLocation(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	legacyPayload, _ := json.Marshal(map[string]any{
		"locationName": "My Home",
		"latitude":     35.85,
		"longitude":    139.55,
	})
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketConfig).Put(configKey, legacyPayload)
	}); err != nil {
		t.Fatalf("put legacy config: %v", err)
	}

	cfg, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	_ = s.Close()

	if len(cfg.Locations) != 1 || cfg.Locations[0].Name != "My Home" || cfg.Locations[0].Latitude != 35.85 {
		t.Fatalf("expected migrated location, got %+v", cfg.Locations)
	}
	if !cfg.Locations[0].IsPrimary {
		t.Fatalf("expected migrated location to be primary")
	}
}

func TestStorePurgesFutureEventsOnOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}

	now := time.Now().UTC()
	if err := s.SaveEvent(EventRecord{
		ID:        "past-ev",
		Type:      "TWEARTH-QUAKE",
		Severity:  "info",
		Title:     "Past",
		Timestamp: now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveEvent past failed: %v", err)
	}
	if err := s.SaveEvent(EventRecord{
		ID:        "future-ev",
		Type:      "TWEARTH-QUAKE",
		Severity:  "info",
		Title:     "Future",
		Timestamp: now.Add(9 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveEvent future failed: %v", err)
	}
	_ = s.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open 2 failed: %v", err)
	}
	defer func() { _ = s2.Close() }()

	evs, err := s2.ListEvents(10)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(evs) != 1 || evs[0].ID != "past-ev" {
		t.Fatalf("expected only past-ev after purge, got %+v", evs)
	}
}

func TestListEventsSinceAndDefaultWindow(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EventWindowHours != 168 {
		t.Fatalf("expected default EventWindowHours=168, got %d", cfg.EventWindowHours)
	}
	cfg.EventWindowHours = 0
	cfg.Normalize()
	if cfg.EventWindowHours != 168 {
		t.Fatalf("expected normalized EventWindowHours=168, got %d", cfg.EventWindowHours)
	}
	cfg.EventWindowHours = -1
	cfg.Normalize()
	if cfg.EventWindowHours != -1 {
		t.Fatalf("expected -1 (all) to be preserved, got %d", cfg.EventWindowHours)
	}

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = s.Close() }()

	now := time.Now().UTC()
	for _, tc := range []struct {
		id string
		ts time.Time
	}{
		{"old-10d", now.Add(-240 * time.Hour)},
		{"mid-3d", now.Add(-72 * time.Hour)},
		{"recent-2h", now.Add(-2 * time.Hour)},
	} {
		if err := s.SaveEvent(EventRecord{
			ID:        tc.id,
			Type:      "TWEARTH-QUAKE",
			Severity:  "info",
			Title:     tc.id,
			Timestamp: tc.ts,
		}); err != nil {
			t.Fatalf("SaveEvent %s failed: %v", tc.id, err)
		}
	}

	evs7d, err := s.ListEventsSince(now.Add(-168*time.Hour), 100)
	if err != nil {
		t.Fatalf("ListEventsSince 7d failed: %v", err)
	}
	if len(evs7d) != 2 || evs7d[0].ID != "recent-2h" || evs7d[1].ID != "mid-3d" {
		t.Fatalf("unexpected 7d events: %+v", evs7d)
	}

	evsAll, err := s.ListEventsSince(time.Time{}, 100)
	if err != nil {
		t.Fatalf("ListEventsSince all failed: %v", err)
	}
	if len(evsAll) != 3 {
		t.Fatalf("expected 3 events for all-time query, got %d", len(evsAll))
	}
}
