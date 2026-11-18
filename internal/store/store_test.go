package store

import (
	"testing"
	"time"
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
	if cfg.Latitude == 0 || cfg.Longitude == 0 {
		t.Fatalf("expected non-zero default coordinates, got %+v", cfg)
	}

	cfg.LocationName = "Tokyo"
	cfg.Latitude = 35.6812
	cfg.Longitude = 139.7671
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := s.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after save failed: %v", err)
	}
	if loaded.LocationName != "Tokyo" || loaded.Latitude != 35.6812 {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}

	if err := s.SaveTimeSeries(TimeSeriesPoint{
		Timestamp:   "202611190500",
		PressureMSL: 1013.2,
		KpIndex:     2.3,
	}); err != nil {
		t.Fatalf("SaveTimeSeries failed: %v", err)
	}

	pts, err := s.ListTimeSeries("202611190000", "202611192359")
	if err != nil {
		t.Fatalf("ListTimeSeries failed: %v", err)
	}
	if len(pts) != 1 || pts[0].PressureMSL != 1013.2 {
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
