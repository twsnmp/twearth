package collector

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func mockHTTPClient(fn func(r *http.Request) (int, string)) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			status, body := fn(r)
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}, nil
		}),
	}
}

func TestOpenMeteoClients(t *testing.T) {
	client := mockHTTPClient(func(r *http.Request) (int, string) {
		switch {
		case strings.Contains(r.URL.Path, "/forecast") && strings.Contains(r.URL.RawQuery, "wind_speed_250hPa"):
			return http.StatusOK, `[{"latitude":35.0,"longitude":140.0,"current":{"wind_speed_250hPa":120.5,"wind_direction_250hPa":270.0,"wind_speed_500hPa":60.0,"wind_direction_500hPa":260.0,"geopotential_height_500hPa":5600.0}}]`
		case strings.Contains(r.URL.Path, "/forecast"):
			return http.StatusOK, `{"current":{"surface_pressure":1008.5,"pressure_msl":1012.3,"temperature_2m":18.4,"relative_humidity_2m":65,"precipitation":2.5,"rain":1.5,"showers":1.0,"snowfall":0.8,"snow_depth":0.12,"weather_code":73,"cape":450.0}}`
		case strings.Contains(r.URL.Path, "/air-quality"):
			return http.StatusOK, `{"current":{"pm2_5":12.1,"pm10":20.5,"dust":3.2,"uv_index":4.0}}`
		case strings.Contains(r.URL.Path, "/marine"):
			return http.StatusOK, `{"latitude":35.5,"longitude":140.5,"current":{"sea_surface_temperature":21.7,"wave_height":1.4,"ocean_current_velocity":0.8,"ocean_current_direction":45}}`
		default:
			return http.StatusNotFound, ""
		}
	})

	c := &OpenMeteoClient{
		Client:         client,
		ForecastBase:   "https://mock.local/forecast",
		MarineBase:     "https://mock.local/marine",
		AirQualityBase: "https://mock.local/air-quality",
	}

	ctx := context.Background()
	wSnap, err := c.FetchLocalWeather(ctx, 35.8569, 139.5492)
	if err != nil {
		t.Fatalf("FetchLocalWeather failed: %v", err)
	}
	if wSnap.PressureMSL != 1012.3 || wSnap.PM25 != 12.1 || wSnap.CAPE != 450.0 ||
		wSnap.Precipitation != 2.5 || wSnap.Rain != 1.5 || wSnap.Showers != 1.0 ||
		wSnap.Snowfall != 0.8 || wSnap.SnowDepth != 0.12 {
		t.Fatalf("unexpected weather snapshot: %+v", wSnap)
	}

	grid, err := c.FetchJetStreamGrid(ctx)
	if err != nil || len(grid) != 1 || grid[0].WindSpeed250hPa != 120.5 {
		t.Fatalf("unexpected wind grid: err=%v grid=%+v", err, grid)
	}

	mar, err := c.FetchMarine(ctx, 35.5, 140.5)
	if err != nil || mar.SeaSurfaceTemperature != 21.7 {
		t.Fatalf("unexpected marine snapshot: err=%v mar=%+v", err, mar)
	}
}

func TestLidenSpaceVolcanoAndQuake(t *testing.T) {
	client := mockHTTPClient(func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/targetTimes_N3.json":
			return http.StatusOK, `[{"basetime":"20261119050000","validtime":"20261119050500"}]`
		case "/targetTimes_N1.json":
			return http.StatusOK, `[{"basetime":"20261119050000","validtime":"20261119050500"}]`
		case "/liden.geojson":
			return http.StatusOK, `{"features":[{"geometry":{"type":"Point","coordinates":[139.55,35.86]},"properties":{"type":1}},{"geometry":{"type":"Point","coordinates":[135.0,34.0]},"properties":{"type":2}}]}`
		case "/kp.json":
			return http.StatusOK, `[{"time_tag":"2026-11-19T05:00:00Z","kp_index":5.33}]`
		case "/xray.json":
			return http.StatusOK, `[{"time_tag":"2026-11-19T05:00:00Z","flux":2.5e-5,"energy":"0.1-0.8nm"}]`
		case "/volcano_list.json":
			return http.StatusOK, `[{"code":"314","name_jp":"富士山","latlon":[35.3606,138.7274]}]`
		case "/warning.json":
			return http.StatusOK, `{"314":{"level":"2","condition":"火口周辺警報"}}`
		case "/usgs.geojson":
			return http.StatusOK, `{"features":[{"id":"us1","properties":{"mag":5.2,"place":"Offshore Chiba","time":1700000000000,"title":"M 5.2 - Offshore Chiba"},"geometry":{"coordinates":[140.8,35.4,30.0]}}]}`
		default:
			return http.StatusNotFound, ""
		}
	})

	ctx := context.Background()

	lc := &LidenClient{
		Client:          client,
		TargetTimesURL:  "https://mock.local/targetTimes_N3.json",
		GeoJSONTemplate: "https://mock.local/liden.geojson?b=%s&v=%s",
	}
	lid, err := lc.FetchLiden(ctx, 35.8569, 139.5492, 20.0)
	if err != nil {
		t.Fatalf("FetchLiden failed: %v", err)
	}
	if lid.TotalCount != 2 || lid.NearbyCount != 1 {
		t.Fatalf("expected total=2 nearby=1, got %+v", lid)
	}

	rc := &RadarClient{
		Client:          client,
		TargetTimesURL:  "https://mock.local/targetTimes_N1.json",
		TileURLTemplate: "https://mock.local/nowc/%s/none/%s/surf/hrpns/{z}/{x}/{y}.png",
	}
	rad, err := rc.FetchRadar(ctx)
	if err != nil {
		t.Fatalf("FetchRadar failed: %v", err)
	}
	if rad.BaseTime != "20261119050000" || !strings.Contains(rad.TileURLTemplate, "20261119050500/surf/hrpns/{z}/{x}/{y}.png") {
		t.Fatalf("unexpected radar info: %+v", rad)
	}

	sc := &SpaceClient{
		Client:  client,
		KpURL:   "https://mock.local/kp.json",
		XRayURL: "https://mock.local/xray.json",
	}
	sp, err := sc.FetchSpaceWeather(ctx)
	if err != nil {
		t.Fatalf("FetchSpaceWeather failed: %v", err)
	}
	if sp.KpIndex != 5.33 || sp.FlareClass != "M" {
		t.Fatalf("unexpected space weather: %+v", sp)
	}

	vc := &VolcanoClient{
		Client:     client,
		ListURL:    "https://mock.local/volcano_list.json",
		WarningURL: "https://mock.local/warning.json",
	}
	vols, err := vc.FetchVolcanoes(ctx)
	if err != nil || len(vols) != 1 || vols[0].AlertLevel != 2 || vols[0].Name != "富士山" {
		t.Fatalf("unexpected volcanoes: err=%v vols=%+v", err, vols)
	}

	qc := &QuakeClient{
		Client:  client,
		USGSURL: "https://mock.local/usgs.geojson",
	}
	usgs, err := qc.FetchUSGS(ctx)
	if err != nil || len(usgs) != 1 || usgs[0].Magnitude != 5.2 {
		t.Fatalf("unexpected USGS events: err=%v usgs=%+v", err, usgs)
	}

	rawP2P := []byte(`{"id":"q1","code":551,"time":"2026/11/19 05:00:00.000","earthquake":{"maxScale":40,"domesticTsunami":"None","hypocenter":{"name":"茨城県南部","latitude":36.1,"longitude":139.9,"depth":50,"magnitude":4.8}}}`)
	ev, ok, err := ParseP2PMessage(rawP2P)
	if err != nil || !ok || ev.MaxScale != "4" || ev.Hypocenter != "茨城県南部" {
		t.Fatalf("unexpected P2P parsed event: ok=%v err=%v ev=%+v", ok, err, ev)
	}
	wantOccurredAt := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	if !ev.OccurredAt.Equal(wantOccurredAt) {
		t.Fatalf("expected OccurredAt=%v, got %v", wantOccurredAt, ev.OccurredAt)
	}

	if got := parseP2PTime("2026/11/20 07:52:02.26"); !got.Equal(time.Date(2026, 11, 19, 22, 52, 2, 260000000, time.UTC)) {
		t.Fatalf("unexpected 2-digit frac parseP2PTime: %v", got)
	}
	if got := parseP2PTime("2026-11-20T07:52:02+09:00"); !got.Equal(time.Date(2026, 11, 19, 22, 52, 2, 0, time.UTC)) {
		t.Fatalf("unexpected RFC3339 parseP2PTime: %v", got)
	}
}

func TestSeverityClassification(t *testing.T) {
	if got := classifyQuakeSeverity(QuakeEvent{Code: 552}, "3"); got != "critical" {
		t.Fatalf("expected tsunami code 552 to be critical, got %s", got)
	}
	if got := classifyQuakeSeverity(QuakeEvent{Code: 556}, "3"); got != "critical" {
		t.Fatalf("expected EEW code 556 to be critical, got %s", got)
	}
	if got := classifyQuakeSeverity(QuakeEvent{Code: 551, MaxScale: "5-", Magnitude: 5.2}, "3"); got != "critical" {
		t.Fatalf("expected intensity 5- to be critical, got %s", got)
	}
	if got := classifyQuakeSeverity(QuakeEvent{Code: 551, MaxScale: "3", Magnitude: 4.0}, "3"); got != "warning" {
		t.Fatalf("expected intensity 3 to be warning, got %s", got)
	}
	if got := classifyQuakeSeverity(QuakeEvent{Code: 551, MaxScale: "2", Magnitude: 3.1}, "3"); got != "info" {
		t.Fatalf("expected intensity 2 M3.1 to be info, got %s", got)
	}
	if got := classifyPressureSeverity(-8.5, 4.0); got != "critical" {
		t.Fatalf("expected -8.5 hPa drop to be critical, got %s", got)
	}
	if got := classifyPressureSeverity(-4.5, 4.0); got != "warning" {
		t.Fatalf("expected -4.5 hPa drop to be warning, got %s", got)
	}
	if got := classifyLightningSeverity(12); got != "critical" {
		t.Fatalf("expected 12 lightning strikes to be critical, got %s", got)
	}
	if got := classifySpaceSeverity(7.3, "M"); got != "critical" {
		t.Fatalf("expected Kp 7.3 to be critical, got %s", got)
	}
	if got := classifySpaceSeverity(5.0, "X"); got != "critical" {
		t.Fatalf("expected X-flare to be critical, got %s", got)
	}
	// QuakeMinIntensity respected when raised to "4"
	if got := classifyQuakeSeverity(QuakeEvent{Code: 551, MaxScale: "3", Magnitude: 4.0}, "4"); got != "info" {
		t.Fatalf("expected intensity 3 to be info when minIntensity=4, got %s", got)
	}
	if got := classifyQuakeSeverity(QuakeEvent{Code: 551, MaxScale: "4", Magnitude: 4.4}, "4"); got != "warning" {
		t.Fatalf("expected intensity 4 to be warning when minIntensity=4, got %s", got)
	}
	// Global USGS M4.7 -> info, M5.2 -> warning
	if got := classifyQuakeSeverity(QuakeEvent{Source: "usgs", MaxScale: "-", Magnitude: 4.7}, "3"); got != "info" {
		t.Fatalf("expected USGS M4.7 to be info, got %s", got)
	}
	if got := classifyQuakeSeverity(QuakeEvent{Source: "usgs", MaxScale: "-", Magnitude: 5.2}, "3"); got != "warning" {
		t.Fatalf("expected USGS M5.2 to be warning, got %s", got)
	}
}

func TestParseP2PTsunamiGrades(t *testing.T) {
	evWatch, ok, err := ParseP2PMessage([]byte(`{"id":"ts1","code":552,"time":"2026/11/22 09:00:00.000","cancelled":false,"areas":[{"grade":"Watch","name":"千葉県九十九里・外房"}]}`))
	if err != nil || !ok || evWatch.Tsunami != "Watch" || evWatch.Title != "津波注意報発表" {
		t.Fatalf("unexpected tsunami watch: ok=%v err=%v ev=%+v", ok, err, evWatch)
	}
	if sev := classifyQuakeSeverity(evWatch, "3"); sev != "warning" {
		t.Fatalf("expected Watch tsunami to be warning, got %s", sev)
	}

	evWarn, ok, err := ParseP2PMessage([]byte(`{"id":"ts2","code":552,"time":"2026/11/22 09:01:00.000","cancelled":false,"areas":[{"grade":"Watch","name":"A"},{"grade":"Warning","name":"B"}]}`))
	if err != nil || !ok || evWarn.Tsunami != "Warning" || evWarn.Title != "津波警報発表" {
		t.Fatalf("unexpected tsunami warning: ok=%v err=%v ev=%+v", ok, err, evWarn)
	}
	if sev := classifyQuakeSeverity(evWarn, "3"); sev != "critical" {
		t.Fatalf("expected Warning tsunami to be critical, got %s", sev)
	}

	evCancel, ok, err := ParseP2PMessage([]byte(`{"id":"ts3","code":552,"time":"2026/11/22 09:02:00.000","cancelled":true,"areas":[]}`))
	if err != nil || !ok || evCancel.Tsunami != "Cancelled" || evCancel.Title != "津波予報解除" {
		t.Fatalf("unexpected tsunami cancel: ok=%v err=%v ev=%+v", ok, err, evCancel)
	}
}

func TestAdjustSeverityByDistance(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.Locations = []store.ObservationLocation{
		{ID: "tokyo", Name: "東京", Latitude: 35.6762, Longitude: 139.6503, IsPrimary: true},
		{ID: "fukuoka", Name: "福岡", Latitude: 33.5902, Longitude: 130.4017, IsPrimary: false},
	}
	cfg.Normalize()

	// Near Fukuoka (0 km) in "nearest" mode -> stays critical
	eff, _, ref := AdjustSeverityByDistance("critical", AlertSeverityMeta{
		Source: "jma_extra", Category: "disaster", Latitude: 33.5902, Longitude: 130.4017,
	}, cfg)
	if eff != "critical" || ref != "福岡" {
		t.Fatalf("expected critical near Fukuoka, got eff=%s ref=%s", eff, ref)
	}

	// Medium distance from Fukuoka (Kagoshima ~220km) -> critical downgrades to warning
	eff, _, _ = AdjustSeverityByDistance("critical", AlertSeverityMeta{
		Source: "jma_extra", Category: "disaster", Latitude: 31.596, Longitude: 130.557,
	}, cfg)
	if eff != "warning" {
		t.Fatalf("expected medium distance critical -> warning, got %s", eff)
	}

	// Far distance (>300km from both Tokyo and Fukuoka, e.g. Sapporo) -> critical downgrades to info
	eff, _, _ = AdjustSeverityByDistance("critical", AlertSeverityMeta{
		Source: "jma_extra", Category: "disaster", Latitude: 43.062, Longitude: 141.354,
	}, cfg)
	if eff != "info" {
		t.Fatalf("expected far distance critical -> info, got %s", eff)
	}

	// Switch to "primary" mode (Tokyo only): Fukuoka alert (~880km from Tokyo) downgrades to info
	cfgPrimary := cfg
	cfgPrimary.AlertDistanceMode = "primary"
	eff, _, _ = AdjustSeverityByDistance("critical", AlertSeverityMeta{
		Source: "jma_extra", Category: "disaster", Latitude: 33.5902, Longitude: 130.4017,
	}, cfgPrimary)
	if eff != "info" {
		t.Fatalf("expected primary mode to attenuate Fukuoka -> info, got %s", eff)
	}

	// Major domestic earthquake exception: Intensity 6- or domestic M7.2 at >300km keeps warning floor
	eff, _, _ = AdjustQuakeSeverityByDistance("critical", QuakeEvent{
		Source: "p2pquake", Code: 551, MaxScale: "6-", Magnitude: 6.8, Latitude: 43.062, Longitude: 141.354,
	}, cfg)
	if eff != "warning" {
		t.Fatalf("expected domestic major quake floor warning, got %s", eff)
	}

	// Distant overseas USGS M7.2 (Chile ~17,000km away) -> drops to info
	eff, _, _ = AdjustQuakeSeverityByDistance("critical", QuakeEvent{
		Source: "usgs", Code: 0, MaxScale: "-", Magnitude: 7.2, Latitude: -33.4, Longitude: -70.6,
	}, cfg)
	if eff != "info" {
		t.Fatalf("expected distant overseas USGS M7.2 -> info, got %s", eff)
	}

	// Space weather (non-geographic) keeps critical
	eff, _, _ = AdjustSeverityByDistance("critical", AlertSeverityMeta{
		Source: "space", Category: "space",
	}, cfg)
	if eff != "critical" {
		t.Fatalf("expected space weather to stay critical, got %s", eff)
	}
}

func TestFetchMultiMarineCellSelectionAndNullFilter(t *testing.T) {
	var capturedQuery string
	client := mockHTTPClient(func(r *http.Request) (int, string) {
		capturedQuery = r.URL.RawQuery
		return http.StatusOK, `[
			{"latitude":35.48,"longitude":139.82,"current":{"sea_surface_temperature":20.5,"wave_height":0.6,"ocean_current_velocity":0.4,"ocean_current_direction":90}},
			{"latitude":36.00,"longitude":139.00,"current":{"sea_surface_temperature":null,"wave_height":null,"ocean_current_velocity":null,"ocean_current_direction":null}}
		]`
	})
	c := &OpenMeteoClient{
		Client:     client,
		MarineBase: "https://mock.local/marine",
	}
	snaps, err := c.FetchMultiMarine(context.Background(), []store.MarineLocation{
		{ID: "tokyo-bay", Name: "東京湾", Latitude: 35.48, Longitude: 139.82},
		{ID: "inland", Name: "陸地", Latitude: 36.00, Longitude: 139.00},
	})
	if err != nil {
		t.Fatalf("FetchMultiMarine failed: %v", err)
	}
	if !strings.Contains(capturedQuery, "cell_selection=sea") {
		t.Fatalf("expected cell_selection=sea in query, got %q", capturedQuery)
	}
	if len(snaps) != 1 || snaps[0].LocationID != "tokyo-bay" || snaps[0].SeaSurfaceTemperature != 20.5 {
		t.Fatalf("expected null marine point filtered out, got %+v", snaps)
	}
}

func TestNearestCoastalSSTSkipsClimateRegions(t *testing.T) {
	marines := []MarineSnapshot{
		{LocationID: "nino-3", Latitude: 0.0, Longitude: -120.0, SeaSurfaceTemperature: 26.8},
		{LocationID: "tokyo-bay", Latitude: 35.48, Longitude: 139.82, SeaSurfaceTemperature: 19.2},
		{LocationID: "oyashio-sanriku", Latitude: 39.50, Longitude: 142.80, SeaSurfaceTemperature: 14.1},
	}
	locs := []store.MarineLocation{
		{ID: "nino-3", Category: "climate"},
		{ID: "tokyo-bay", Category: "nearshore"},
		{ID: "oyashio-sanriku", Category: "regional"},
	}

	got := nearestCoastalSST(35.6762, 139.6503, marines, locs)
	if got != 19.2 {
		t.Fatalf("expected Tokyo nearest coastal SST 19.2, got %.1f", got)
	}
	gotNorth := nearestCoastalSST(39.7, 141.1, marines, locs)
	if gotNorth != 14.1 {
		t.Fatalf("expected northern Japan nearest coastal SST 14.1, got %.1f", gotNorth)
	}
}
