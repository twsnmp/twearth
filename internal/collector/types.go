package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const defaultUserAgent = "twearth/0.1.0 (+https://github.com/twsnmp/twearth)"

// WeatherSnapshot holds current local atmospheric and air-quality observations.
type WeatherSnapshot struct {
	Timestamp          time.Time `json:"timestamp"`
	SurfacePressure    float64   `json:"surfacePressure"`
	PressureMSL        float64   `json:"pressureMsl"`
	Temperature2m      float64   `json:"temperature2m"`
	RelativeHumidity2m float64   `json:"relativeHumidity2m"`
	Precipitation      float64   `json:"precipitation"`
	WeatherCode        int       `json:"weatherCode"`
	CAPE               float64   `json:"cape"`
	ShortwaveRadiation float64   `json:"shortwaveRadiation"`
	SoilTemp0cm        float64   `json:"soilTemp0cm"`
	SoilMoisture       float64   `json:"soilMoisture"`
	PM25               float64   `json:"pm25"`
	PM10               float64   `json:"pm10"`
	Dust               float64   `json:"dust"`
	UVIndex            float64   `json:"uvIndex"`
}

// WindGridPoint represents upper-air wind vectors at a grid coordinate.
type WindGridPoint struct {
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	WindSpeed250hPa       float64 `json:"windSpeed250hPa"`
	WindDirection250hPa   float64 `json:"windDirection250hPa"`
	WindSpeed500hPa       float64 `json:"windSpeed500hPa"`
	WindDirection500hPa   float64 `json:"windDirection500hPa"`
	GeopotentialHeight500 float64 `json:"geopotentialHeight500"`
}

// MarineSnapshot holds sea surface temperature and wave/current metrics.
type MarineSnapshot struct {
	Timestamp             time.Time `json:"timestamp"`
	Latitude              float64   `json:"latitude"`
	Longitude             float64   `json:"longitude"`
	SeaSurfaceTemperature float64   `json:"seaSurfaceTemperature"`
	WaveHeight            float64   `json:"waveHeight"`
	OceanCurrentVelocity  float64   `json:"oceanCurrentVelocity"`
	OceanCurrentDirection float64   `json:"oceanCurrentDirection"`
}

// LightningStrike represents a single LIDEN lightning discharge.
type LightningStrike struct {
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Type       string  `json:"type"`
	DistanceKm float64 `json:"distanceKm"`
}

// LidenSnapshot holds recent lightning strikes and count within the configured radius.
type LidenSnapshot struct {
	ValidTime   string            `json:"validTime"`
	TotalCount  int               `json:"totalCount"`
	NearbyCount int               `json:"nearbyCount"`
	RadiusKm    float64           `json:"radiusKm"`
	Strikes     []LightningStrike `json:"strikes"`
}

// VolcanoInfo represents an active volcano and its current warning level.
type VolcanoInfo struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	AlertLevel   int     `json:"alertLevel"`
	WarningTitle string  `json:"warningTitle,omitempty"`
}

// SpaceWeatherSnapshot holds NOAA SWPC Kp index and GOES X-ray flux.
type SpaceWeatherSnapshot struct {
	Timestamp  time.Time `json:"timestamp"`
	KpIndex    float64   `json:"kpIndex"`
	XRayFlux   float64   `json:"xRayFlux"`
	FlareClass string    `json:"flareClass"`
}

// QuakeEvent represents a normalized earthquake/tsunami event from P2PQuake or USGS.
type QuakeEvent struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Code        int       `json:"code,omitempty"`
	Title       string    `json:"title"`
	Hypocenter  string    `json:"hypocenter"`
	Magnitude   float64   `json:"magnitude"`
	MaxScale    string    `json:"maxScale"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	DepthKm     float64   `json:"depthKm"`
	Tsunami     string    `json:"tsunami,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func fetchJSON(ctx context.Context, client *http.Client, url string, dst any) error {
	body, err := fetchBytes(ctx, client, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode json from %s: %w", url, err)
	}
	return nil
}

func fetchBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "application/json, application/xml, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
}

// HaversineKm calculates the great-circle distance in kilometers between two WGS84 coordinates.
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}
