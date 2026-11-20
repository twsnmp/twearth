package collector

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
			return http.StatusOK, `{"current":{"surface_pressure":1008.5,"pressure_msl":1012.3,"temperature_2m":18.4,"relative_humidity_2m":65,"precipitation":0.2,"weather_code":3,"cape":450.0}}`
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
	if wSnap.PressureMSL != 1012.3 || wSnap.PM25 != 12.1 || wSnap.CAPE != 450.0 {
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
