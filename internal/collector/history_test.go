package collector

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

func jsonResp(status int, body string, headers ...string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		h.Set(headers[i], headers[i+1])
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestFetchWeatherAndMarineHistoryRange(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/archive", "/forecast":
				return jsonResp(http.StatusOK, `{
					"hourly": {
						"time": ["2026-10-01T00:00", "2026-10-01T01:00"],
						"pressure_msl": [1012.5, 1013.0],
						"temperature_2m": [21.0, 20.5],
						"precipitation": [0.0, 1.2],
						"rain": [0.0, 1.2],
						"snowfall": [0.0, 0.0],
						"snow_depth": [0.0, 0.0],
						"cape": [100.0, 150.0]
					}
				}`), nil
			case "/marine":
				return jsonResp(http.StatusOK, `{
					"hourly": {
						"time": ["2026-10-01T00:00", "2026-10-01T01:00"],
						"sea_surface_temperature": [23.4, 23.5],
						"wave_height": [1.1, 1.2],
						"ocean_current_velocity": [0.45, 0.50],
						"ocean_current_direction": [90.0, 95.0]
					}
				}`), nil
			default:
				return jsonResp(http.StatusNotFound, `{}`), nil
			}
		}),
	}

	rl := &RateLimiter{limiters: map[ProviderID]*providerLimiter{
		ProviderOpenMeteo: {minInterval: time.Millisecond},
	}}
	cli := &OpenMeteoClient{
		Client:       client,
		Limiter:      rl,
		ForecastBase: "https://mock.local/forecast",
		ArchiveBase:  "https://mock.local/archive",
		MarineBase:   "https://mock.local/marine",
	}

	loc := store.ObservationLocation{ID: "tokyo", Name: "Tokyo", Latitude: 35.67, Longitude: 139.65}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 23, 0, 0, 0, time.UTC)

	wPts, err := cli.FetchWeatherHistoryRange(context.Background(), loc, start, end, true)
	if err != nil {
		t.Fatalf("FetchWeatherHistoryRange: %v", err)
	}
	if len(wPts) != 2 || wPts[0].Temperature2m != 21.0 || wPts[1].Precipitation != 1.2 {
		t.Fatalf("unexpected weather points: %+v", wPts)
	}

	mLoc := store.MarineLocation{ID: "sagami", Name: "Sagami", Latitude: 35.15, Longitude: 139.35}
	mPts, err := cli.FetchMarineHistoryRange(context.Background(), mLoc, start, end)
	if err != nil {
		t.Fatalf("FetchMarineHistoryRange: %v", err)
	}
	if len(mPts) != 2 || mPts[0].SeaSurfaceTemperature != 23.4 || mPts[1].WaveHeight != 1.2 {
		t.Fatalf("unexpected marine points: %+v", mPts)
	}
}

func TestRateLimit429RetryAndCatchUpGapFill(t *testing.T) {
	var attempts atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			n := attempts.Add(1)
			if n == 1 {
				return jsonResp(http.StatusTooManyRequests, `{}`, "Retry-After", "0"), nil
			}
			return jsonResp(http.StatusOK, `{
				"hourly": {
					"time": ["2026-11-20T06:00", "2026-11-20T07:00"],
					"sea_surface_temperature": [21.8, 21.9],
					"wave_height": [0.8, 0.9],
					"ocean_current_velocity": [0.3, 0.35],
					"ocean_current_direction": [120.0, 125.0]
				}
			}`), nil
		}),
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	oldTime := time.Now().UTC().Add(-6 * time.Hour).Format("200601021504")
	if err := st.SaveMarineTimeSeries(store.MarineTimeSeriesPoint{
		LocationID:            "sagami",
		Timestamp:             oldTime,
		SeaSurfaceTemperature: 21.5,
	}); err != nil {
		t.Fatalf("SaveMarineTimeSeries: %v", err)
	}

	rl := &RateLimiter{limiters: map[ProviderID]*providerLimiter{
		ProviderOpenMeteo: {minInterval: time.Millisecond},
	}}
	mgr := NewManager(st)
	mgr.limiter = rl
	mgr.openMeteo = &OpenMeteoClient{
		Client:     client,
		Limiter:    rl,
		MarineBase: "https://mock.local/marine",
	}

	loc := store.MarineLocation{ID: "sagami", Name: "Sagami", Latitude: 35.15, Longitude: 139.35}
	mgr.catchUpMarineLocation(context.Background(), loc, time.Now().UTC())

	if attempts.Load() < 2 {
		t.Fatalf("expected retry after 429, attempts=%d", attempts.Load())
	}
	_, _, count, err := st.GetTimeSeriesBounds("marine", "sagami")
	if err != nil {
		t.Fatalf("GetTimeSeriesBounds: %v", err)
	}
	if count < 2 {
		t.Fatalf("expected gap-fill points to be saved, count=%d", count)
	}
}

func TestFetchDailyWeatherAndMarineAggregation(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/archive":
				return jsonResp(http.StatusOK, `{
					"daily": {
						"time": ["2025-01-01", "2025-01-02"],
						"temperature_2m_mean": [5.2, 6.1],
						"temperature_2m_max": [9.0, 10.0],
						"temperature_2m_min": [1.5, 2.0],
						"pressure_msl_mean": [1018.0, 1015.5],
						"precipitation_sum": [0.0, 4.5],
						"snowfall_sum": [0.0, 0.0],
						"wind_speed_10m_max": [5.5, 7.2]
					}
				}`), nil
			case "/marine":
				return jsonResp(http.StatusOK, `{
					"hourly": {
						"time": ["2025-01-01T00:00", "2025-01-01T12:00"],
						"sea_surface_temperature": [18.0, 19.0],
						"wave_height": [1.0, 1.8],
						"ocean_current_velocity": [0.4, 0.6],
						"ocean_current_direction": [90.0, 95.0]
					}
				}`), nil
			default:
				return jsonResp(http.StatusNotFound, `{}`), nil
			}
		}),
	}

	rl := &RateLimiter{limiters: map[ProviderID]*providerLimiter{
		ProviderOpenMeteo: {minInterval: time.Millisecond},
	}}
	cli := &OpenMeteoClient{
		Client:      client,
		Limiter:     rl,
		ArchiveBase: "https://mock.local/archive",
		MarineBase:  "https://mock.local/marine",
	}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)

	dWeather, err := cli.FetchWeatherDailyRange(context.Background(), store.ObservationLocation{ID: "tokyo"}, start, end, true)
	if err != nil {
		t.Fatalf("FetchWeatherDailyRange: %v", err)
	}
	if len(dWeather) != 2 || dWeather[0].Date != "20250101" || dWeather[1].PrecipitationSum != 4.5 {
		t.Fatalf("unexpected daily weather: %+v", dWeather)
	}

	dMarine, err := cli.FetchMarineDailyRange(context.Background(), store.MarineLocation{ID: "sagami"}, start, end)
	if err != nil {
		t.Fatalf("FetchMarineDailyRange: %v", err)
	}
	if len(dMarine) != 1 || dMarine[0].SSTMean != 18.5 || dMarine[0].SSTMin != 18.0 || dMarine[0].SSTMax != 19.0 || dMarine[0].WaveHeightMax != 1.8 {
		t.Fatalf("unexpected daily marine: %+v", dMarine)
	}
}

func TestFetchMarineDailySkipsNullSSTWithoutZeroFill(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResp(http.StatusOK, `{
				"hourly": {
					"time": [
						"2021-10-01T00:00",
						"2021-10-01T12:00",
						"2023-01-01T00:00",
						"2023-01-01T12:00"
					],
					"sea_surface_temperature": [null, null, 16.0, 17.0],
					"wave_height": [0.5, 0.9, 1.1, 1.3],
					"ocean_current_velocity": [null, null, 0.4, 0.6],
					"ocean_current_direction": [null, null, 90.0, 95.0]
				}
			}`), nil
		}),
	}

	rl := &RateLimiter{limiters: map[ProviderID]*providerLimiter{
		ProviderOpenMeteo: {minInterval: time.Millisecond},
	}}
	cli := &OpenMeteoClient{
		Client:     client,
		Limiter:    rl,
		MarineBase: "https://mock.local/marine",
	}

	start := time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 1, 1, 23, 0, 0, 0, time.UTC)

	dMarine, err := cli.FetchMarineDailyRange(context.Background(), store.MarineLocation{ID: "tokyo-bay"}, start, end)
	if err != nil {
		t.Fatalf("FetchMarineDailyRange: %v", err)
	}
	if len(dMarine) != 2 {
		t.Fatalf("expected 2 daily marine records, got %d: %+v", len(dMarine), dMarine)
	}
	if !dMarine[0].MissingSST || dMarine[0].MissingWave || !dMarine[0].MissingCurrent || dMarine[0].WaveHeightMax != 0.9 {
		t.Fatalf("unexpected 2021-10-01 daily marine: %+v", dMarine[0])
	}
	if dMarine[1].MissingSST || dMarine[1].SSTMean != 16.5 || dMarine[1].WaveHeightMax != 1.3 {
		t.Fatalf("unexpected 2023-01-01 daily marine: %+v", dMarine[1])
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	if err := st.SaveDailyMarineBatch(dMarine); err != nil {
		t.Fatalf("SaveDailyMarineBatch: %v", err)
	}

	sstRes, err := st.QueryHistory(store.HistoryQuery{
		Kind:        "marine",
		LocationID:  "tokyo-bay",
		Metric:      "seaSurfaceTemperature",
		Granularity: "daily",
	})
	if err != nil {
		t.Fatalf("QueryHistory SST: %v", err)
	}
	if len(sstRes.Points) != 1 || sstRes.Stats.Count != 1 || sstRes.Stats.Min != 16.5 {
		t.Fatalf("expected only 2023-01-01 SST point without 0C fill, got points=%+v stats=%+v", sstRes.Points, sstRes.Stats)
	}

	waveRes, err := st.QueryHistory(store.HistoryQuery{
		Kind:        "marine",
		LocationID:  "tokyo-bay",
		Metric:      "waveHeight",
		Granularity: "daily",
	})
	if err != nil {
		t.Fatalf("QueryHistory waveHeight: %v", err)
	}
	if len(waveRes.Points) != 2 || waveRes.Stats.Count != 2 {
		t.Fatalf("expected 2 waveHeight points, got points=%+v stats=%+v", waveRes.Points, waveRes.Stats)
	}
}
