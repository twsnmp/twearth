package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// OpenMeteoClient fetches weather, upper-air/synoptic grids, marine SST, air quality, and geocoding data.
type OpenMeteoClient struct {
	Client         *http.Client
	Limiter        *RateLimiter
	ForecastBase   string
	ArchiveBase    string
	MarineBase     string
	AirQualityBase string
	GeocodingBase  string
}

// NewOpenMeteoClient creates an OpenMeteoClient with default public endpoints.
func NewOpenMeteoClient(rl *RateLimiter) *OpenMeteoClient {
	if rl == nil {
		rl = NewRateLimiter()
	}
	return &OpenMeteoClient{
		Client:         newHTTPClient(25 * time.Second),
		Limiter:        rl,
		ForecastBase:   "https://api.open-meteo.com/v1/forecast",
		ArchiveBase:    "https://archive-api.open-meteo.com/v1/archive",
		MarineBase:     "https://marine-api.open-meteo.com/v1/marine",
		AirQualityBase: "https://air-quality-api.open-meteo.com/v1/air-quality",
		GeocodingBase:  "https://geocoding-api.open-meteo.com/v1/search",
	}
}

type openMeteoForecastResp struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Current   struct {
		SurfacePressure    float64 `json:"surface_pressure"`
		PressureMSL        float64 `json:"pressure_msl"`
		Temperature2m      float64 `json:"temperature_2m"`
		RelativeHumidity2m float64 `json:"relative_humidity_2m"`
		Precipitation      float64 `json:"precipitation"`
		Rain               float64 `json:"rain"`
		Showers            float64 `json:"showers"`
		Snowfall           float64 `json:"snowfall"`
		SnowDepth          float64 `json:"snow_depth"`
		WeatherCode        int     `json:"weather_code"`
		WindSpeed10m       float64 `json:"wind_speed_10m"`
		WindDirection10m   float64 `json:"wind_direction_10m"`
		CAPE               float64 `json:"cape"`
		ShortwaveRadiation float64 `json:"shortwave_radiation"`
		SoilTemp0cm        float64 `json:"soil_temperature_0cm"`
		SoilMoisture       float64 `json:"soil_moisture_0_to_1cm"`
	} `json:"current"`
	Hourly struct {
		Time        []string  `json:"time"`
		PressureMSL []float64 `json:"pressure_msl"`
	} `json:"hourly"`
}

type openMeteoAirQualityResp struct {
	Current struct {
		PM25    float64 `json:"pm2_5"`
		PM10    float64 `json:"pm10"`
		Dust    float64 `json:"dust"`
		UVIndex float64 `json:"uv_index"`
	} `json:"current"`
}

func decodeSingleOrArray[T any](body []byte) ([]T, error) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var list []T
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var single T
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, err
	}
	return []T{single}, nil
}

// FetchLocalWeather retrieves current weather and air quality at (lat, lon).
func (c *OpenMeteoClient) FetchLocalWeather(ctx context.Context, lat, lon float64) (WeatherSnapshot, error) {
	snaps, err := c.FetchMultiWeather(ctx, []store.ObservationLocation{
		{
			ID:        "single",
			Name:      "Local",
			Latitude:  lat,
			Longitude: lon,
		},
	})
	if err != nil {
		return WeatherSnapshot{}, err
	}
	if len(snaps) == 0 {
		return WeatherSnapshot{}, fmt.Errorf("empty weather response")
	}
	return snaps[0], nil
}

// FetchMultiWeather retrieves current weather and air quality for all locations in batched requests.
func (c *OpenMeteoClient) FetchMultiWeather(ctx context.Context, locs []store.ObservationLocation) ([]WeatherSnapshot, error) {
	if len(locs) == 0 {
		return nil, nil
	}

	lats := make([]string, len(locs))
	lons := make([]string, len(locs))
	for i, loc := range locs {
		lats[i] = fmt.Sprintf("%.4f", loc.Latitude)
		lons[i] = fmt.Sprintf("%.4f", loc.Longitude)
	}

	fURL := fmt.Sprintf(
		"%s?latitude=%s&longitude=%s&current=surface_pressure,pressure_msl,temperature_2m,relative_humidity_2m,precipitation,rain,showers,snowfall,snow_depth,weather_code,wind_speed_10m,wind_direction_10m,cape,shortwave_radiation,soil_temperature_0cm,soil_moisture_0_to_1cm&hourly=pressure_msl&past_hours=3&forecast_hours=1&timezone=UTC",
		c.ForecastBase, strings.Join(lats, ","), strings.Join(lons, ","),
	)
	body, err := fetchBytesLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, fURL)
	if err != nil {
		return nil, err
	}
	fList, err := decodeSingleOrArray[openMeteoForecastResp](body)
	if err != nil {
		return nil, fmt.Errorf("decode multi-weather: %w", err)
	}

	var aqList []openMeteoAirQualityResp
	aqURL := fmt.Sprintf(
		"%s?latitude=%s&longitude=%s&current=pm2_5,pm10,dust,uv_index&timezone=UTC",
		c.AirQualityBase, strings.Join(lats, ","), strings.Join(lons, ","),
	)
	if aqBody, err := fetchBytesLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, aqURL); err == nil {
		if parsed, decErr := decodeSingleOrArray[openMeteoAirQualityResp](aqBody); decErr == nil {
			aqList = parsed
		}
	}

	now := time.Now().UTC()
	out := make([]WeatherSnapshot, 0, len(locs))
	for i, loc := range locs {
		if i >= len(fList) {
			break
		}
		f := fList[i]
		delta3h := 0.0
		if len(f.Hourly.PressureMSL) >= 4 {
			// Hourly array with past_hours=3&forecast_hours=1 has index 0 at T-3h and current at index 3 (or last).
			oldP := f.Hourly.PressureMSL[0]
			if oldP > 0 && f.Current.PressureMSL > 0 {
				delta3h = f.Current.PressureMSL - oldP
			}
		}

		snap := WeatherSnapshot{
			LocationID:         loc.ID,
			LocationName:       loc.Name,
			Latitude:           loc.Latitude,
			Longitude:          loc.Longitude,
			Timestamp:          now,
			SurfacePressure:    f.Current.SurfacePressure,
			PressureMSL:        f.Current.PressureMSL,
			PressureDelta3h:    delta3h,
			Temperature2m:      f.Current.Temperature2m,
			RelativeHumidity2m: f.Current.RelativeHumidity2m,
			Precipitation:      f.Current.Precipitation,
			Rain:               f.Current.Rain,
			Showers:            f.Current.Showers,
			Snowfall:           f.Current.Snowfall,
			SnowDepth:          f.Current.SnowDepth,
			WeatherCode:        f.Current.WeatherCode,
			WindSpeed10m:       f.Current.WindSpeed10m,
			WindDirection10m:   f.Current.WindDirection10m,
			CAPE:               f.Current.CAPE,
			ShortwaveRadiation: f.Current.ShortwaveRadiation,
			SoilTemp0cm:        f.Current.SoilTemp0cm,
			SoilMoisture:       f.Current.SoilMoisture,
		}
		if i < len(aqList) {
			snap.PM25 = aqList[i].Current.PM25
			snap.PM10 = aqList[i].Current.PM10
			snap.Dust = aqList[i].Current.Dust
			snap.UVIndex = aqList[i].Current.UVIndex
		}
		out = append(out, snap)
	}
	return out, nil
}

type openMeteoWindPointResp struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Current   struct {
		PressureMSL           float64 `json:"pressure_msl"`
		WindSpeed10m          float64 `json:"wind_speed_10m"`
		WindDirection10m      float64 `json:"wind_direction_10m"`
		WindSpeed250hPa       float64 `json:"wind_speed_250hPa"`
		WindDirection250hPa   float64 `json:"wind_direction_250hPa"`
		WindSpeed500hPa       float64 `json:"wind_speed_500hPa"`
		WindDirection500hPa   float64 `json:"wind_direction_500hPa"`
		GeopotentialHeight500 float64 `json:"geopotential_height_500hPa"`
	} `json:"current"`
}

// FetchJetStreamGrid fetches 250hPa/500hPa wind vectors and surface pressure over East Asia (20-50N, 120-155E, 5-deg step).
func (c *OpenMeteoClient) FetchJetStreamGrid(ctx context.Context) ([]WindGridPoint, error) {
	var lats, lons []string
	for lat := 20.0; lat <= 50.0; lat += 5.0 {
		for lon := 120.0; lon <= 155.0; lon += 5.0 {
			lats = append(lats, fmt.Sprintf("%.1f", lat))
			lons = append(lons, fmt.Sprintf("%.1f", lon))
		}
	}

	u := fmt.Sprintf(
		"%s?latitude=%s&longitude=%s&current=pressure_msl,wind_speed_10m,wind_direction_10m,wind_speed_250hPa,wind_direction_250hPa,wind_speed_500hPa,wind_direction_500hPa,geopotential_height_500hPa&timezone=UTC",
		c.ForecastBase, strings.Join(lats, ","), strings.Join(lons, ","),
	)

	var items []openMeteoWindPointResp
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, u, &items); err != nil {
		return nil, err
	}

	out := make([]WindGridPoint, 0, len(items))
	for _, it := range items {
		out = append(out, WindGridPoint{
			Latitude:              it.Latitude,
			Longitude:             it.Longitude,
			PressureMSL:           it.Current.PressureMSL,
			WindSpeed10m:          it.Current.WindSpeed10m,
			WindDirection10m:      it.Current.WindDirection10m,
			WindSpeed250hPa:       it.Current.WindSpeed250hPa,
			WindDirection250hPa:   it.Current.WindDirection250hPa,
			WindSpeed500hPa:       it.Current.WindSpeed500hPa,
			WindDirection500hPa:   it.Current.WindDirection500hPa,
			GeopotentialHeight500: it.Current.GeopotentialHeight500,
		})
	}
	return out, nil
}

type openMeteoMarineResp struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Current   struct {
		SeaSurfaceTemperature *float64 `json:"sea_surface_temperature"`
		WaveHeight            *float64 `json:"wave_height"`
		OceanCurrentVelocity  *float64 `json:"ocean_current_velocity"`
		OceanCurrentDirection *float64 `json:"ocean_current_direction"`
	} `json:"current"`
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// FetchMarine fetches sea surface temperature and wave data for a single coastal/ocean point.
func (c *OpenMeteoClient) FetchMarine(ctx context.Context, lat, lon float64) (MarineSnapshot, error) {
	snaps, err := c.FetchMultiMarine(ctx, []store.MarineLocation{
		{
			ID:        "single-marine",
			Name:      "Marine",
			Latitude:  lat,
			Longitude: lon,
		},
	})
	if err != nil {
		return MarineSnapshot{}, err
	}
	if len(snaps) == 0 {
		return MarineSnapshot{}, fmt.Errorf("empty marine response")
	}
	return snaps[0], nil
}

// FetchMultiMarine retrieves sea surface temperature and wave/current metrics for all marine locations in one batch.
func (c *OpenMeteoClient) FetchMultiMarine(ctx context.Context, locs []store.MarineLocation) ([]MarineSnapshot, error) {
	if len(locs) == 0 {
		return nil, nil
	}

	lats := make([]string, len(locs))
	lons := make([]string, len(locs))
	for i, loc := range locs {
		lats[i] = fmt.Sprintf("%.4f", loc.Latitude)
		lons[i] = fmt.Sprintf("%.4f", loc.Longitude)
	}

	u := fmt.Sprintf(
		"%s?latitude=%s&longitude=%s&current=sea_surface_temperature,wave_height,ocean_current_velocity,ocean_current_direction&cell_selection=sea&timezone=UTC",
		c.MarineBase, strings.Join(lats, ","), strings.Join(lons, ","),
	)
	body, err := fetchBytesLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, u)
	if err != nil {
		return nil, err
	}
	list, err := decodeSingleOrArray[openMeteoMarineResp](body)
	if err != nil {
		return nil, fmt.Errorf("decode multi-marine: %w", err)
	}

	now := time.Now().UTC()
	out := make([]MarineSnapshot, 0, len(locs))
	for i, loc := range locs {
		if i >= len(list) {
			break
		}
		resp := list[i]
		if resp.Current.SeaSurfaceTemperature == nil && resp.Current.WaveHeight == nil {
			continue
		}
		out = append(out, MarineSnapshot{
			LocationID:            loc.ID,
			LocationName:          loc.Name,
			Timestamp:             now,
			Latitude:              loc.Latitude,
			Longitude:             loc.Longitude,
			SeaSurfaceTemperature: derefFloat(resp.Current.SeaSurfaceTemperature),
			WaveHeight:            derefFloat(resp.Current.WaveHeight),
			OceanCurrentVelocity:  derefFloat(resp.Current.OceanCurrentVelocity),
			OceanCurrentDirection: derefFloat(resp.Current.OceanCurrentDirection),
			MissingSST:            resp.Current.SeaSurfaceTemperature == nil,
			MissingWave:           resp.Current.WaveHeight == nil,
			MissingCurrent:        resp.Current.OceanCurrentVelocity == nil && resp.Current.OceanCurrentDirection == nil,
		})
	}
	return out, nil
}

type openMeteoGeocodingResp struct {
	Results []CitySearchResult `json:"results"`
}

// SearchCities queries Open-Meteo Geocoding API for matching cities.
func (c *OpenMeteoClient) SearchCities(ctx context.Context, query, lang string) ([]CitySearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if lang != "ja" && lang != "en" {
		lang = "en"
	}
	u := fmt.Sprintf(
		"%s?name=%s&count=8&language=%s&format=json",
		c.GeocodingBase, url.QueryEscape(q), lang,
	)
	var resp openMeteoGeocodingResp
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderOpenMeteo, u, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}
