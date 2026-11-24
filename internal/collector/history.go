package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

type openMeteoWeatherHistoryResp struct {
	Hourly struct {
		Time          []string   `json:"time"`
		PressureMSL   []*float64 `json:"pressure_msl"`
		Temperature2m []*float64 `json:"temperature_2m"`
		Precipitation []*float64 `json:"precipitation"`
		Rain          []*float64 `json:"rain"`
		Snowfall      []*float64 `json:"snowfall"`
		SnowDepth     []*float64 `json:"snow_depth"`
		CAPE          []*float64 `json:"cape"`
	} `json:"hourly"`
}

type openMeteoMarineHistoryResp struct {
	Hourly struct {
		Time                  []string   `json:"time"`
		SeaSurfaceTemperature []*float64 `json:"sea_surface_temperature"`
		WaveHeight            []*float64 `json:"wave_height"`
		OceanCurrentVelocity  []*float64 `json:"ocean_current_velocity"`
		OceanCurrentDirection []*float64 `json:"ocean_current_direction"`
	} `json:"hourly"`
}

func floatAt(arr []*float64, idx int) (float64, bool) {
	if idx < 0 || idx >= len(arr) || arr[idx] == nil {
		return 0, false
	}
	return *arr[idx], true
}

func parseOpenMeteoHour(raw string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02T15:04", raw)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// FetchWeatherHistoryRange retrieves hourly weather history for a single location within [startDate, endDate] (inclusive).
// When useArchive is true, it queries the Open-Meteo Historical Archive API; otherwise it queries the Forecast API.
func (c *OpenMeteoClient) FetchWeatherHistoryRange(
	ctx context.Context,
	loc store.ObservationLocation,
	startDate, endDate time.Time,
	useArchive bool,
) ([]store.TimeSeriesPoint, error) {
	base := c.ForecastBase
	if useArchive {
		base = c.ArchiveBase
	}
	if base == "" {
		return nil, fmt.Errorf("missing open-meteo base url")
	}

	u := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&hourly=pressure_msl,temperature_2m,precipitation,rain,snowfall,snow_depth,cape&timezone=UTC",
		base,
		loc.Latitude,
		loc.Longitude,
		startDate.UTC().Format("2006-01-02"),
		endDate.UTC().Format("2006-01-02"),
	)

	var resp openMeteoWeatherHistoryResp
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, u, &resp); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]store.TimeSeriesPoint, 0, len(resp.Hourly.Time))
	for i, rawTime := range resp.Hourly.Time {
		t, ok := parseOpenMeteoHour(rawTime)
		if !ok || t.After(now) {
			continue
		}
		temp, hasTemp := floatAt(resp.Hourly.Temperature2m, i)
		pres, hasPres := floatAt(resp.Hourly.PressureMSL, i)
		if !hasTemp && !hasPres {
			continue
		}
		precip, _ := floatAt(resp.Hourly.Precipitation, i)
		rain, _ := floatAt(resp.Hourly.Rain, i)
		snow, _ := floatAt(resp.Hourly.Snowfall, i)
		depth, _ := floatAt(resp.Hourly.SnowDepth, i)
		cape, _ := floatAt(resp.Hourly.CAPE, i)

		out = append(out, store.TimeSeriesPoint{
			LocationID:    loc.ID,
			Timestamp:     t.Format("200601021504"),
			PressureMSL:   pres,
			Temperature2m: temp,
			Precipitation: precip,
			Rain:          rain,
			Snowfall:      snow,
			SnowDepth:     depth,
			CAPE:          cape,
		})
	}
	return out, nil
}

// FetchMarineHistoryRange retrieves hourly marine observations (SST, wave height, current) within [startDate, endDate].
func (c *OpenMeteoClient) FetchMarineHistoryRange(
	ctx context.Context,
	loc store.MarineLocation,
	startDate, endDate time.Time,
) ([]store.MarineTimeSeriesPoint, error) {
	if c.MarineBase == "" {
		return nil, fmt.Errorf("missing open-meteo marine base url")
	}

	u := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&hourly=sea_surface_temperature,wave_height,ocean_current_velocity,ocean_current_direction&cell_selection=sea&timezone=UTC",
		c.MarineBase,
		loc.Latitude,
		loc.Longitude,
		startDate.UTC().Format("2006-01-02"),
		endDate.UTC().Format("2006-01-02"),
	)

	var resp openMeteoMarineHistoryResp
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, u, &resp); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]store.MarineTimeSeriesPoint, 0, len(resp.Hourly.Time))
	for i, rawTime := range resp.Hourly.Time {
		t, ok := parseOpenMeteoHour(rawTime)
		if !ok || t.After(now) {
			continue
		}
		sst, hasSST := floatAt(resp.Hourly.SeaSurfaceTemperature, i)
		wave, hasWave := floatAt(resp.Hourly.WaveHeight, i)
		vel, hasVel := floatAt(resp.Hourly.OceanCurrentVelocity, i)
		dir, hasDir := floatAt(resp.Hourly.OceanCurrentDirection, i)
		if !hasSST && !hasWave && !hasVel {
			continue
		}

		out = append(out, store.MarineTimeSeriesPoint{
			LocationID:            loc.ID,
			Timestamp:             t.Format("200601021504"),
			SeaSurfaceTemperature: sst,
			WaveHeight:            wave,
			OceanCurrentVelocity:  vel,
			OceanCurrentDirection: dir,
			MissingSST:            !hasSST,
			MissingWave:           !hasWave,
			MissingCurrent:        !hasVel && !hasDir,
		})
	}
	return out, nil
}

type openMeteoWeatherDailyResp struct {
	Daily struct {
		Time             []string   `json:"time"`
		TemperatureMean  []*float64 `json:"temperature_2m_mean"`
		TemperatureMax   []*float64 `json:"temperature_2m_max"`
		TemperatureMin   []*float64 `json:"temperature_2m_min"`
		PressureMSLMean  []*float64 `json:"pressure_msl_mean"`
		PrecipitationSum []*float64 `json:"precipitation_sum"`
		SnowfallSum      []*float64 `json:"snowfall_sum"`
		WindSpeedMax     []*float64 `json:"wind_speed_10m_max"`
	} `json:"daily"`
}

// FetchWeatherDailyRange retrieves daily climate summaries (up to 1+ year per request) within [startDate, endDate].
func (c *OpenMeteoClient) FetchWeatherDailyRange(
	ctx context.Context,
	loc store.ObservationLocation,
	startDate, endDate time.Time,
	useArchive bool,
) ([]store.DailyWeatherPoint, error) {
	base := c.ForecastBase
	if useArchive {
		base = c.ArchiveBase
	}
	if base == "" {
		return nil, fmt.Errorf("missing open-meteo base url")
	}

	u := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&daily=temperature_2m_mean,temperature_2m_max,temperature_2m_min,pressure_msl_mean,precipitation_sum,snowfall_sum,wind_speed_10m_max&timezone=UTC",
		base,
		loc.Latitude,
		loc.Longitude,
		startDate.UTC().Format("2006-01-02"),
		endDate.UTC().Format("2006-01-02"),
	)

	var resp openMeteoWeatherDailyResp
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, u, &resp); err != nil {
		return nil, err
	}

	today := time.Now().UTC().Format("2006-01-02")
	out := make([]store.DailyWeatherPoint, 0, len(resp.Daily.Time))
	for i, rawDate := range resp.Daily.Time {
		if rawDate > today || len(rawDate) != 10 {
			continue
		}
		tMean, hasMean := floatAt(resp.Daily.TemperatureMean, i)
		pMean, hasPres := floatAt(resp.Daily.PressureMSLMean, i)
		if !hasMean && !hasPres {
			continue
		}
		tMax, _ := floatAt(resp.Daily.TemperatureMax, i)
		tMin, _ := floatAt(resp.Daily.TemperatureMin, i)
		precip, _ := floatAt(resp.Daily.PrecipitationSum, i)
		snow, _ := floatAt(resp.Daily.SnowfallSum, i)
		wind, _ := floatAt(resp.Daily.WindSpeedMax, i)

		out = append(out, store.DailyWeatherPoint{
			LocationID:       loc.ID,
			Date:             strings.ReplaceAll(rawDate, "-", ""),
			TempMean:         tMean,
			TempMax:          tMax,
			TempMin:          tMin,
			PressureMSLMean:  pMean,
			PrecipitationSum: precip,
			SnowfallSum:      snow,
			WindSpeedMax:     wind,
		})
	}
	return out, nil
}

// FetchMarineDailyRange fetches hourly marine data for [startDate, endDate] and aggregates it into daily summaries.
func (c *OpenMeteoClient) FetchMarineDailyRange(
	ctx context.Context,
	loc store.MarineLocation,
	startDate, endDate time.Time,
) ([]store.DailyMarinePoint, error) {
	hourly, err := c.FetchMarineHistoryRange(ctx, loc, startDate, endDate)
	if err != nil {
		return nil, err
	}
	if len(hourly) == 0 {
		return nil, nil
	}

	type dayAgg struct {
		sstSum, sstMin, sstMax float64
		sstCount               int
		waveMax                float64
		waveCount              int
		velSum                 float64
		velCount               int
	}
	byDate := make(map[string]*dayAgg)
	var dates []string

	for _, pt := range hourly {
		if len(pt.Timestamp) < 8 {
			continue
		}
		d := pt.Timestamp[:8]
		agg, ok := byDate[d]
		if !ok {
			agg = &dayAgg{sstMin: math.MaxFloat64, sstMax: -math.MaxFloat64}
			byDate[d] = agg
			dates = append(dates, d)
		}
		if !pt.MissingSST && !math.IsNaN(pt.SeaSurfaceTemperature) {
			agg.sstSum += pt.SeaSurfaceTemperature
			if pt.SeaSurfaceTemperature < agg.sstMin {
				agg.sstMin = pt.SeaSurfaceTemperature
			}
			if pt.SeaSurfaceTemperature > agg.sstMax {
				agg.sstMax = pt.SeaSurfaceTemperature
			}
			agg.sstCount++
		}
		if !pt.MissingWave && !math.IsNaN(pt.WaveHeight) {
			if agg.waveCount == 0 || pt.WaveHeight > agg.waveMax {
				agg.waveMax = pt.WaveHeight
			}
			agg.waveCount++
		}
		if !pt.MissingCurrent && !math.IsNaN(pt.OceanCurrentVelocity) {
			agg.velSum += pt.OceanCurrentVelocity
			agg.velCount++
		}
	}

	sort.Strings(dates)
	out := make([]store.DailyMarinePoint, 0, len(dates))
	for _, d := range dates {
		agg := byDate[d]
		if agg.sstCount == 0 && agg.waveCount == 0 && agg.velCount == 0 {
			continue
		}
		dp := store.DailyMarinePoint{
			LocationID:     loc.ID,
			Date:           d,
			MissingSST:     agg.sstCount == 0,
			MissingWave:    agg.waveCount == 0,
			MissingCurrent: agg.velCount == 0,
		}
		if agg.sstCount > 0 {
			dp.SSTMean = math.Round((agg.sstSum/float64(agg.sstCount))*100) / 100
			dp.SSTMin = math.Round(agg.sstMin*100) / 100
			dp.SSTMax = math.Round(agg.sstMax*100) / 100
		}
		if agg.waveCount > 0 {
			dp.WaveHeightMax = math.Round(agg.waveMax*100) / 100
		}
		if agg.velCount > 0 {
			dp.CurrentVelocityMean = math.Round((agg.velSum/float64(agg.velCount))*100) / 100
		}
		out = append(out, dp)
	}
	return out, nil
}

// FetchUSGSHistoryRange retrieves M4.0+ earthquakes from USGS FDSN Event API within [startTime, endTime].
func (c *QuakeClient) FetchUSGSHistoryRange(
	ctx context.Context,
	rl *RateLimiter,
	startTime, endTime time.Time,
	minMag float64,
) ([]QuakeEvent, error) {
	if minMag <= 0 {
		minMag = 4.0
	}
	base := "https://earthquake.usgs.gov/fdsnws/event/1/query"
	if strings.Contains(c.USGSURL, "127.0.0.1") || strings.Contains(c.USGSURL, "localhost") {
		base = c.USGSURL
	}
	u := fmt.Sprintf(
		"%s?format=geojson&starttime=%s&endtime=%s&minmagnitude=%.1f&limit=1000&orderby=time",
		base,
		startTime.UTC().Format("2006-01-02T15:04:05"),
		endTime.UTC().Format("2006-01-02T15:04:05"),
		minMag,
	)
	var geo usgsGeoJSON
	if err := fetchJSONLimited(ctx, c.Client, rl, ProviderUSGS, u, &geo); err != nil {
		return nil, err
	}
	out := make([]QuakeEvent, 0, len(geo.Features))
	for _, f := range geo.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		depth := 0.0
		if len(f.Geometry.Coordinates) >= 3 {
			depth = f.Geometry.Coordinates[2]
		}
		out = append(out, QuakeEvent{
			ID:         "usgs-" + f.ID,
			Source:     "usgs",
			Title:      f.Properties.Title,
			Hypocenter: f.Properties.Place,
			Magnitude:  f.Properties.Mag,
			MaxScale:   "-",
			Longitude:  f.Geometry.Coordinates[0],
			Latitude:   f.Geometry.Coordinates[1],
			DepthKm:    depth,
			OccurredAt: time.UnixMilli(f.Properties.Time).UTC(),
		})
	}
	return out, nil
}

// FetchP2PHistoryPage fetches a page of P2PQuake history with limit & offset under rate limiting.
func (c *QuakeClient) FetchP2PHistoryPage(ctx context.Context, rl *RateLimiter, limit, offset int) ([]QuakeEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	base := "https://api.p2pquake.net/v2/history"
	if strings.Contains(c.P2PHistoryURL, "127.0.0.1") || strings.Contains(c.P2PHistoryURL, "localhost") {
		base = strings.Split(c.P2PHistoryURL, "?")[0]
	}
	u := fmt.Sprintf("%s?codes=551&limit=%d&offset=%d", base, limit, offset)
	raw, err := fetchBytesLimited(ctx, c.Client, rl, ProviderP2PQuake, u)
	if err != nil {
		return nil, err
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	out := make([]QuakeEvent, 0, len(msgs))
	for _, m := range msgs {
		if ev, ok, err := ParseP2PMessage(m); err == nil && ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

// FetchSpaceHistory retrieves ~7 days of 3-hourly Kp index records from NOAA SWPC for gap catch-up.
func (c *SpaceClient) FetchSpaceHistory(ctx context.Context, rl *RateLimiter) ([]SpaceWeatherSnapshot, error) {
	u := "https://services.swpc.noaa.gov/products/noaa-planetary-k-index.json"
	if strings.Contains(c.KpURL, "127.0.0.1") || strings.Contains(c.KpURL, "localhost") {
		u = c.KpURL
	}
	raw, err := fetchBytesLimited(ctx, c.Client, rl, ProviderNOAA, u)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	var out []SpaceWeatherSnapshot
	for i, row := range rows {
		if i == 0 || len(row) < 2 {
			continue // skip header
		}
		t, err := time.Parse("2006-01-02 15:04:05.000", row[0])
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", row[0])
			if err != nil {
				continue
			}
		}
		kp, err := strconv.ParseFloat(row[1], 64)
		if err != nil {
			continue
		}
		out = append(out, SpaceWeatherSnapshot{
			Timestamp: t.UTC(),
			KpIndex:   kp,
		})
	}
	return out, nil
}

// FetchLidenRecentSlots fetches any available recent LIDEN GeoJSON slots newer than sinceValidTime (YYYYMMDDHHmmss).
func (c *LidenClient) FetchLidenRecentSlots(ctx context.Context, rl *RateLimiter, homeLat, homeLon, radiusKm float64, sinceValidTime string, maxSlots int) ([]LidenSnapshot, error) {
	if maxSlots <= 0 {
		maxSlots = 6
	}
	raw, err := fetchBytesLimited(ctx, c.Client, rl, ProviderJMA, c.TargetTimesURL)
	if err != nil {
		return nil, err
	}
	var times []lidenTargetTime
	if err := json.Unmarshal(raw, &times); err != nil {
		return nil, err
	}
	var out []LidenSnapshot
	for _, slot := range times {
		if len(out) >= maxSlots {
			break
		}
		if len(slot.Elements) > 0 && !containsElement(slot.Elements, "liden") {
			continue
		}
		if sinceValidTime != "" && slot.ValidTime <= sinceValidTime {
			continue
		}
		u := fmt.Sprintf(c.GeoJSONTemplate, slot.BaseTime, slot.ValidTime)
		var geo lidenGeoJSON
		if err := fetchJSONLimited(ctx, c.Client, rl, ProviderJMA, u, &geo); err != nil {
			continue
		}
		nearby := 0
		for _, f := range geo.Features {
			if len(f.Geometry.Coordinates) < 2 {
				continue
			}
			if HaversineKm(homeLat, homeLon, f.Geometry.Coordinates[1], f.Geometry.Coordinates[0]) <= radiusKm {
				nearby++
			}
		}
		out = append(out, LidenSnapshot{
			ValidTime:   slot.ValidTime,
			TotalCount:  len(geo.Features),
			NearbyCount: nearby,
			RadiusKm:    radiusKm,
		})
	}
	return out, nil
}
