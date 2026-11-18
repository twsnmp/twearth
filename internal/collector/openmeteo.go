package collector

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenMeteoClient fetches weather, upper-air jet stream grid, marine SST, and air quality data.
type OpenMeteoClient struct {
	Client         *http.Client
	ForecastBase   string
	MarineBase     string
	AirQualityBase string
}

// NewOpenMeteoClient creates an OpenMeteoClient with default public endpoints.
func NewOpenMeteoClient() *OpenMeteoClient {
	return &OpenMeteoClient{
		Client:         newHTTPClient(25 * time.Second),
		ForecastBase:   "https://api.open-meteo.com/v1/forecast",
		MarineBase:     "https://marine-api.open-meteo.com/v1/marine",
		AirQualityBase: "https://air-quality-api.open-meteo.com/v1/air-quality",
	}
}

type openMeteoForecastResp struct {
	Current struct {
		SurfacePressure    float64 `json:"surface_pressure"`
		PressureMSL        float64 `json:"pressure_msl"`
		Temperature2m      float64 `json:"temperature_2m"`
		RelativeHumidity2m float64 `json:"relative_humidity_2m"`
		Precipitation      float64 `json:"precipitation"`
		WeatherCode        int     `json:"weather_code"`
		CAPE               float64 `json:"cape"`
		ShortwaveRadiation float64 `json:"shortwave_radiation"`
		SoilTemp0cm        float64 `json:"soil_temperature_0cm"`
		SoilMoisture       float64 `json:"soil_moisture_0_to_1cm"`
	} `json:"current"`
}

type openMeteoAirQualityResp struct {
	Current struct {
		PM25    float64 `json:"pm2_5"`
		PM10    float64 `json:"pm10"`
		Dust    float64 `json:"dust"`
		UVIndex float64 `json:"uv_index"`
	} `json:"current"`
}

// FetchLocalWeather retrieves current weather and air quality at (lat, lon).
func (c *OpenMeteoClient) FetchLocalWeather(ctx context.Context, lat, lon float64) (WeatherSnapshot, error) {
	url := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&current=surface_pressure,pressure_msl,temperature_2m,relative_humidity_2m,precipitation,weather_code,cape,shortwave_radiation,soil_temperature_0cm,soil_moisture_0_to_1cm&timezone=UTC",
		c.ForecastBase, lat, lon,
	)
	var fResp openMeteoForecastResp
	if err := fetchJSON(ctx, c.Client, url, &fResp); err != nil {
		return WeatherSnapshot{}, err
	}

	snap := WeatherSnapshot{
		Timestamp:          time.Now().UTC(),
		SurfacePressure:    fResp.Current.SurfacePressure,
		PressureMSL:        fResp.Current.PressureMSL,
		Temperature2m:      fResp.Current.Temperature2m,
		RelativeHumidity2m: fResp.Current.RelativeHumidity2m,
		Precipitation:      fResp.Current.Precipitation,
		WeatherCode:        fResp.Current.WeatherCode,
		CAPE:               fResp.Current.CAPE,
		ShortwaveRadiation: fResp.Current.ShortwaveRadiation,
		SoilTemp0cm:        fResp.Current.SoilTemp0cm,
		SoilMoisture:       fResp.Current.SoilMoisture,
	}

	aqURL := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&current=pm2_5,pm10,dust,uv_index&timezone=UTC",
		c.AirQualityBase, lat, lon,
	)
	var aqResp openMeteoAirQualityResp
	if err := fetchJSON(ctx, c.Client, aqURL, &aqResp); err == nil {
		snap.PM25 = aqResp.Current.PM25
		snap.PM10 = aqResp.Current.PM10
		snap.Dust = aqResp.Current.Dust
		snap.UVIndex = aqResp.Current.UVIndex
	}

	return snap, nil
}

type openMeteoWindPointResp struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Current   struct {
		WindSpeed250hPa       float64 `json:"wind_speed_250hPa"`
		WindDirection250hPa   float64 `json:"wind_direction_250hPa"`
		WindSpeed500hPa       float64 `json:"wind_speed_500hPa"`
		WindDirection500hPa   float64 `json:"wind_direction_500hPa"`
		GeopotentialHeight500 float64 `json:"geopotential_height_500hPa"`
	} `json:"current"`
}

// FetchJetStreamGrid fetches 250hPa and 500hPa wind vectors over Japan and East Asia (20-50N, 120-155E, 5-deg step).
func (c *OpenMeteoClient) FetchJetStreamGrid(ctx context.Context) ([]WindGridPoint, error) {
	var lats, lons []string
	for lat := 20.0; lat <= 50.0; lat += 5.0 {
		for lon := 120.0; lon <= 155.0; lon += 5.0 {
			lats = append(lats, fmt.Sprintf("%.1f", lat))
			lons = append(lons, fmt.Sprintf("%.1f", lon))
		}
	}

	url := fmt.Sprintf(
		"%s?latitude=%s&longitude=%s&current=wind_speed_250hPa,wind_direction_250hPa,wind_speed_500hPa,wind_direction_500hPa,geopotential_height_500hPa&timezone=UTC",
		c.ForecastBase, strings.Join(lats, ","), strings.Join(lons, ","),
	)

	var items []openMeteoWindPointResp
	if err := fetchJSON(ctx, c.Client, url, &items); err != nil {
		return nil, err
	}

	out := make([]WindGridPoint, 0, len(items))
	for _, it := range items {
		out = append(out, WindGridPoint{
			Latitude:              it.Latitude,
			Longitude:             it.Longitude,
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
		SeaSurfaceTemperature float64 `json:"sea_surface_temperature"`
		WaveHeight            float64 `json:"wave_height"`
		OceanCurrentVelocity  float64 `json:"ocean_current_velocity"`
		OceanCurrentDirection float64 `json:"ocean_current_direction"`
	} `json:"current"`
}

// FetchMarine fetches sea surface temperature and wave data for the nearest coastal/ocean point.
func (c *OpenMeteoClient) FetchMarine(ctx context.Context, lat, lon float64) (MarineSnapshot, error) {
	url := fmt.Sprintf(
		"%s?latitude=%.4f&longitude=%.4f&current=sea_surface_temperature,wave_height,ocean_current_velocity,ocean_current_direction&timezone=UTC",
		c.MarineBase, lat, lon,
	)
	var resp openMeteoMarineResp
	if err := fetchJSON(ctx, c.Client, url, &resp); err != nil {
		return MarineSnapshot{}, err
	}
	return MarineSnapshot{
		Timestamp:             time.Now().UTC(),
		Latitude:              resp.Latitude,
		Longitude:             resp.Longitude,
		SeaSurfaceTemperature: resp.Current.SeaSurfaceTemperature,
		WaveHeight:            resp.Current.WaveHeight,
		OceanCurrentVelocity:  resp.Current.OceanCurrentVelocity,
		OceanCurrentDirection: resp.Current.OceanCurrentDirection,
	}, nil
}
