package store

import (
	"testing"
	"time"
)

func TestMarineAndWeatherHistoryQueryAndStats(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Save 48 hours of marine SST data (linearly increasing from 20.0 to 24.7)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var mPts []MarineTimeSeriesPoint
	for i := 0; i < 48; i++ {
		ts := base.Add(time.Duration(i) * time.Hour).Format("200601021504")
		mPts = append(mPts, MarineTimeSeriesPoint{
			LocationID:            "sagami-bay",
			Timestamp:             ts,
			SeaSurfaceTemperature: 20.0 + float64(i)*0.1,
			WaveHeight:            1.0 + float64(i%5)*0.2,
		})
	}
	if err := st.SaveMarineTimeSeriesBatch(mPts); err != nil {
		t.Fatalf("SaveMarineTimeSeriesBatch: %v", err)
	}

	earliest, latest, count, err := st.GetTimeSeriesBounds("marine", "sagami-bay")
	if err != nil {
		t.Fatalf("GetTimeSeriesBounds: %v", err)
	}
	if count != 48 || earliest != "202610010000" || latest != "202610022300" {
		t.Fatalf("unexpected bounds: earliest=%s latest=%s count=%d", earliest, latest, count)
	}

	res, err := st.QueryHistory(HistoryQuery{
		Kind:        "marine",
		LocationID:  "sagami-bay",
		Metric:      "seaSurfaceTemperature",
		Granularity: "daily",
	})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if res.Stats.Count != 48 {
		t.Fatalf("expected 48 samples, got %d", res.Stats.Count)
	}
	if res.Stats.Min != 20.0 || res.Stats.Max != 24.7 {
		t.Fatalf("unexpected min/max: min=%.2f max=%.2f", res.Stats.Min, res.Stats.Max)
	}
	if res.Stats.Mean < 22.3 || res.Stats.Mean > 22.4 {
		t.Fatalf("unexpected mean: %.2f", res.Stats.Mean)
	}
	if len(res.Points) != 2 {
		t.Fatalf("expected 2 daily buckets, got %d", len(res.Points))
	}
	if res.Points[0].MovingAvg == nil || res.Points[1].MovingAvg == nil {
		t.Fatalf("expected moving average to be populated")
	}
	if res.Stats.TrendPerDay < 2.3 || res.Stats.TrendPerDay > 2.5 {
		t.Fatalf("expected ~2.4/day trend, got %.4f", res.Stats.TrendPerDay)
	}
}

func TestQuakeHistoryQuery(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	records := []QuakeRecord{
		{
			ID:         "q1",
			Source:     "p2pquake",
			Hypocenter: "Tokyo Bay",
			Magnitude:  4.2,
			MaxScale:   "3",
			Latitude:   35.60,
			Longitude:  139.80,
			OccurredAt: now.Add(-48 * time.Hour),
		},
		{
			ID:         "q2",
			Source:     "usgs",
			Hypocenter: "Offshore",
			Magnitude:  6.1,
			MaxScale:   "5-",
			Latitude:   35.70,
			Longitude:  139.70,
			OccurredAt: now.Add(-24 * time.Hour),
		},
		{
			ID:         "q3-far",
			Source:     "usgs",
			Hypocenter: "Alaska",
			Magnitude:  7.0,
			Latitude:   61.0,
			Longitude:  -150.0,
			OccurredAt: now.Add(-12 * time.Hour),
		},
	}
	if err := st.SaveQuakeBatch(records); err != nil {
		t.Fatalf("SaveQuakeBatch: %v", err)
	}

	res, err := st.QueryQuakeHistory(QuakeHistoryQuery{
		Latitude:    35.6762,
		Longitude:   139.6503,
		RadiusKm:    100,
		FromTime:    now.Add(-72 * time.Hour),
		ToTime:      now,
		Granularity: "daily",
	})
	if err != nil {
		t.Fatalf("QueryQuakeHistory: %v", err)
	}
	if res.TotalCount != 2 {
		t.Fatalf("expected 2 nearby quakes, got %d", res.TotalCount)
	}
	if res.MaxMagnitude != 6.1 || res.MaxScale != "5-" {
		t.Fatalf("unexpected max stats: mag=%.1f scale=%s", res.MaxMagnitude, res.MaxScale)
	}
}

func TestDailyClimateHistoryAndClearData(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Verify Config.Normalize allows -1 (All) and multi-year options
	cfg := DefaultConfig()
	cfg.HistoryDays = -1
	cfg.Normalize()
	if cfg.HistoryDays != -1 || cfg.HourlyHistoryDays != 90 {
		t.Fatalf("unexpected normalized config: HistoryDays=%d HourlyHistoryDays=%d", cfg.HistoryDays, cfg.HourlyHistoryDays)
	}

	// Save 400 days of daily weather data
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var dPts []DailyWeatherPoint
	for i := 0; i < 400; i++ {
		d := base.AddDate(0, 0, i).Format("20060102")
		dPts = append(dPts, DailyWeatherPoint{
			LocationID:      "tokyo",
			Date:            d,
			TempMean:        15.0 + float64(i)*0.01,
			PressureMSLMean: 1013.0,
		})
	}
	if err := st.SaveDailyWeatherBatch(dPts); err != nil {
		t.Fatalf("SaveDailyWeatherBatch: %v", err)
	}

	res, err := st.QueryHistory(HistoryQuery{
		Kind:        "weather",
		LocationID:  "tokyo",
		FromTS:      "202501010000",
		ToTS:        "202603010000",
		Metric:      "temperature2m",
		Granularity: "auto",
	})
	if err != nil {
		t.Fatalf("QueryHistory daily: %v", err)
	}
	if res.Stats.Count != 400 {
		t.Fatalf("expected 400 daily points, got %d", res.Stats.Count)
	}
	if res.Granularity != "monthly" {
		t.Fatalf("expected monthly auto granularity for 400 days, got %s", res.Granularity)
	}

	// Save an event to ensure ClearHistoryData preserves config and events
	if err := st.SaveEvent(EventRecord{ID: "ev1", Type: "TEST", Title: "Keep me"}); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := st.ClearHistoryData("weather"); err != nil {
		t.Fatalf("ClearHistoryData: %v", err)
	}
	_, _, dCount, err := st.GetDailyBounds("weather", "tokyo")
	if err != nil {
		t.Fatalf("GetDailyBounds after clear: %v", err)
	}
	if dCount != 0 {
		t.Fatalf("expected 0 daily points after clear, got %d", dCount)
	}
	evs, err := st.ListEvents(10)
	if err != nil || len(evs) != 1 {
		t.Fatalf("expected events to be preserved, got %d err=%v", len(evs), err)
	}
	gotCfg, err := st.GetConfig()
	if err != nil || gotCfg.HistoryDays != -1 {
		t.Fatalf("expected config to be preserved, got %+v err=%v", gotCfg, err)
	}
}
