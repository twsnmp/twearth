package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const defaultUserAgent = "twearth/0.2.0 (+https://github.com/twsnmp/twearth)"

// WeatherSnapshot holds current local atmospheric and air-quality observations for a location.
type WeatherSnapshot struct {
	LocationID         string    `json:"locationId"`
	LocationName       string    `json:"locationName"`
	Latitude           float64   `json:"latitude"`
	Longitude          float64   `json:"longitude"`
	Timestamp          time.Time `json:"timestamp"`
	SurfacePressure    float64   `json:"surfacePressure"`
	PressureMSL        float64   `json:"pressureMsl"`
	PressureDelta3h    float64   `json:"pressureDelta3h"`
	Temperature2m      float64   `json:"temperature2m"`
	RelativeHumidity2m float64   `json:"relativeHumidity2m"`
	Precipitation      float64   `json:"precipitation"`
	Rain               float64   `json:"rain"`
	Showers            float64   `json:"showers"`
	Snowfall           float64   `json:"snowfall"`
	SnowDepth          float64   `json:"snowDepth"`
	WeatherCode        int       `json:"weatherCode"`
	WindSpeed10m       float64   `json:"windSpeed10m"`
	WindDirection10m   float64   `json:"windDirection10m"`
	CAPE               float64   `json:"cape"`
	ShortwaveRadiation float64   `json:"shortwaveRadiation"`
	SoilTemp0cm        float64   `json:"soilTemp0cm"`
	SoilMoisture       float64   `json:"soilMoisture"`
	PM25               float64   `json:"pm25"`
	PM10               float64   `json:"pm10"`
	Dust               float64   `json:"dust"`
	UVIndex            float64   `json:"uvIndex"`
}

// WindGridPoint represents upper-air and surface weather vectors at a grid coordinate.
type WindGridPoint struct {
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	PressureMSL           float64 `json:"pressureMsl"`
	WindSpeed10m          float64 `json:"windSpeed10m"`
	WindDirection10m      float64 `json:"windDirection10m"`
	WindSpeed250hPa       float64 `json:"windSpeed250hPa"`
	WindDirection250hPa   float64 `json:"windDirection250hPa"`
	WindSpeed500hPa       float64 `json:"windSpeed500hPa"`
	WindDirection500hPa   float64 `json:"windDirection500hPa"`
	GeopotentialHeight500 float64 `json:"geopotentialHeight500"`
}

// IsobarLine represents a contour line of equal sea-level pressure (hPa).
type IsobarLine struct {
	Pressure    float64     `json:"pressure"`
	IsMajor     bool        `json:"isMajor"`
	Coordinates [][]float64 `json:"coordinates"` // [lon, lat] pairs
}

// PressureCenter represents a detected High (H) or Low (L) pressure center.
type PressureCenter struct {
	Kind      string  `json:"kind"` // "H" | "L"
	Pressure  float64 `json:"pressure"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// SynopticData holds computed surface pressure analysis (isobars + H/L centers).
type SynopticData struct {
	Timestamp time.Time        `json:"timestamp"`
	Isobars   []IsobarLine     `json:"isobars"`
	Centers   []PressureCenter `json:"centers"`
}

// MarineSnapshot holds sea surface temperature and wave/current metrics for a marine point.
type MarineSnapshot struct {
	LocationID            string    `json:"locationId"`
	LocationName          string    `json:"locationName"`
	Timestamp             time.Time `json:"timestamp"`
	Latitude              float64   `json:"latitude"`
	Longitude             float64   `json:"longitude"`
	SeaSurfaceTemperature float64   `json:"seaSurfaceTemperature"`
	WaveHeight            float64   `json:"waveHeight"`
	OceanCurrentVelocity  float64   `json:"oceanCurrentVelocity"`
	OceanCurrentDirection float64   `json:"oceanCurrentDirection"`
}

// CitySearchResult represents a geocoding lookup result from Open-Meteo Geocoding API.
type CitySearchResult struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Admin1    string  `json:"admin1"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
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

// RadarTileInfo holds JMA High-Resolution Precipitation Nowcast raster tile metadata.
type RadarTileInfo struct {
	BaseTime        string `json:"baseTime"`
	ValidTime       string `json:"validTime"`
	TileURLTemplate string `json:"tileUrlTemplate"`
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
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Code       int       `json:"code,omitempty"`
	Title      string    `json:"title"`
	Hypocenter string    `json:"hypocenter"`
	Magnitude  float64   `json:"magnitude"`
	MaxScale   string    `json:"maxScale"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	DepthKm    float64   `json:"depthKm"`
	Tsunami    string    `json:"tsunami,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// KikikuruLayerTile holds tile metadata for a single JMA Kikikuru risk distribution layer.
type KikikuruLayerTile struct {
	Kind            string `json:"kind"` // "land" | "inund" | "flood"
	BaseTime        string `json:"baseTime"`
	ValidTime       string `json:"validTime"`
	TileURLTemplate string `json:"tileUrlTemplate"`
}

// KikikuruInfo holds latest JMA Kikikuru (危険度分布) tile URLs for landslide, inundation, and flood.
type KikikuruInfo struct {
	Land  *KikikuruLayerTile `json:"land,omitempty"`
	Inund *KikikuruLayerTile `json:"inund,omitempty"`
	Flood *KikikuruLayerTile `json:"flood,omitempty"`
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

// IntensityRank converts JMA seismic intensity strings ("1", "5-", "5+", "6-", "6+", "7") to a comparable integer rank.
func IntensityRank(s string) int {
	switch strings.TrimSpace(s) {
	case "1":
		return 10
	case "2":
		return 20
	case "3":
		return 30
	case "4":
		return 40
	case "5-", "5弱":
		return 45
	case "5+", "5強":
		return 50
	case "6-", "6弱":
		return 55
	case "6+", "6強":
		return 60
	case "7":
		return 70
	default:
		return 0
	}
}
