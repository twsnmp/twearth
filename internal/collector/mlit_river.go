package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// MlitRiverClient fetches JMA AMeDAS precipitation/snow station observations
// near configured locations for flood/landslide/avalanche monitoring.
// (MLIT river.go.jp blocks automated polling with HTTP 403, so official JMA AMeDAS JSON is used.)
type MlitRiverClient struct {
	Client        *http.Client
	Limiter       *RateLimiter
	Cache         *HTTPCache
	TableURL      string
	LatestTimeURL string
	MapURLFmt     string
	mu            sync.Mutex
	stations      map[string]amedasStationMeta
	masterFetched time.Time
}

// NewMlitRiverClient creates a MlitRiverClient backed by official JMA AMeDAS endpoints.
func NewMlitRiverClient(rl *RateLimiter, cache *HTTPCache) *MlitRiverClient {
	return &MlitRiverClient{
		Client:        newHTTPClient(20 * time.Second),
		Limiter:       rl,
		Cache:         cache,
		TableURL:      "https://www.jma.go.jp/bosai/amedas/const/amedastable.json",
		LatestTimeURL: "https://www.jma.go.jp/bosai/amedas/data/latest_time.txt",
		MapURLFmt:     "https://www.jma.go.jp/bosai/amedas/data/map/%s.json",
		stations:      make(map[string]amedasStationMeta),
	}
}

type amedasStationMeta struct {
	ID        string
	Name      string
	Type      string
	Latitude  float64
	Longitude float64
}

type amedasTableEntry struct {
	Type   string    `json:"type"`
	KjName string    `json:"kjName"`
	EnName string    `json:"enName"`
	Lat    []float64 `json:"lat"`
	Lon    []float64 `json:"lon"`
}

type amedasObsEntry struct {
	Precipitation1h  []any `json:"precipitation1h"`
	Precipitation24h []any `json:"precipitation24h"`
	Snow             []any `json:"snow"`
	Temp             []any `json:"temp"`
}

func (c *MlitRiverClient) ensureMaster(ctx context.Context) error {
	c.mu.Lock()
	fresh := len(c.stations) > 0 && time.Since(c.masterFetched) < 24*time.Hour
	c.mu.Unlock()
	if fresh {
		return nil
	}

	raw, notModified, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderJMA, c.Cache, c.TableURL)
	if err != nil {
		return err
	}
	if notModified {
		c.mu.Lock()
		c.masterFetched = time.Now()
		c.mu.Unlock()
		return nil
	}

	parsed, err := ParseAmedasTable(raw)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.stations = parsed
	c.masterFetched = time.Now()
	c.mu.Unlock()
	return nil
}

// ParseAmedasTable parses JMA amedastable.json into station metadata keyed by station ID.
func ParseAmedasTable(raw []byte) (map[string]amedasStationMeta, error) {
	var tbl map[string]amedasTableEntry
	if err := json.Unmarshal(raw, &tbl); err != nil {
		return nil, fmt.Errorf("decode amedastable: %w", err)
	}
	out := make(map[string]amedasStationMeta, len(tbl))
	for id, entry := range tbl {
		if len(entry.Lat) < 2 || len(entry.Lon) < 2 {
			continue
		}
		lat := entry.Lat[0] + entry.Lat[1]/60.0
		lon := entry.Lon[0] + entry.Lon[1]/60.0
		name := strings.TrimSpace(entry.KjName)
		if name == "" {
			name = strings.TrimSpace(entry.EnName)
		}
		out[id] = amedasStationMeta{
			ID:        id,
			Name:      name,
			Type:      entry.Type,
			Latitude:  lat,
			Longitude: lon,
		}
	}
	return out, nil
}

// FetchRiverLevels retrieves latest precipitation/water-risk observations across Japan
// (all stations near configured locations, all major A/B observatories nationwide, plus any station reporting rainfall).
func (c *MlitRiverClient) FetchRiverLevels(ctx context.Context, locations []store.ObservationLocation, radiusKm float64) ([]store.RiverTimeSeriesPoint, []store.DisasterRecord, error) {
	if radiusKm <= 0 {
		radiusKm = 30.0
	}
	if err := c.ensureMaster(ctx); err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	stations := c.stations
	c.mu.Unlock()
	if len(stations) == 0 {
		return nil, nil, nil
	}

	// Mark stations near configured locations or major A/B stations as always included.
	alwaysInclude := make(map[string]bool)
	for _, st := range stations {
		if st.Type == "A" || st.Type == "B" {
			alwaysInclude[st.ID] = true
			continue
		}
		for _, loc := range locations {
			if HaversineKm(loc.Latitude, loc.Longitude, st.Latitude, st.Longitude) <= radiusKm {
				alwaysInclude[st.ID] = true
				break
			}
		}
	}

	timeRaw, err := fetchBytesLimited(ctx, c.Client, c.Limiter, ProviderJMA, c.LatestTimeURL)
	if err != nil {
		return nil, nil, err
	}
	obsTime, err := time.Parse(time.RFC3339, strings.TrimSpace(string(timeRaw)))
	if err != nil {
		return nil, nil, fmt.Errorf("parse amedas latest_time: %w", err)
	}

	mapKey := obsTime.In(jstZone).Format("20060102150405")
	mapURL := fmt.Sprintf(c.MapURLFmt, mapKey)
	raw, notModified, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderJMA, c.Cache, mapURL)
	if err != nil {
		return nil, nil, err
	}
	if notModified {
		return nil, nil, nil
	}

	return ParseAmedasMapFiltered(raw, obsTime.UTC(), stations, alwaysInclude)
}

// ParseAmedasMap parses JMA AMeDAS map JSON for all given target stations into RiverTimeSeriesPoints and alerts.
func ParseAmedasMap(raw []byte, obsAt time.Time, targets map[string]amedasStationMeta) ([]store.RiverTimeSeriesPoint, []store.DisasterRecord, error) {
	alwaysInclude := make(map[string]bool, len(targets))
	for id := range targets {
		alwaysInclude[id] = true
	}
	return ParseAmedasMapFiltered(raw, obsAt, targets, alwaysInclude)
}

// ParseAmedasMapFiltered parses JMA AMeDAS map JSON, keeping stations in alwaysInclude or any station with active rainfall.
func ParseAmedasMapFiltered(raw []byte, obsAt time.Time, stations map[string]amedasStationMeta, alwaysInclude map[string]bool) ([]store.RiverTimeSeriesPoint, []store.DisasterRecord, error) {
	var data map[string]amedasObsEntry
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, nil, fmt.Errorf("decode amedas map: %w", err)
	}

	const warnRainMmH = 30.0
	const dangerRainMmH = 50.0

	var points []store.RiverTimeSeriesPoint
	var alerts []store.DisasterRecord

	for id, meta := range stations {
		obs, ok := data[id]
		if !ok {
			continue
		}
		rain1h, hasRain := extractAmedasFloat(obs.Precipitation1h)
		if !hasRain {
			rain1h = 0
		}
		rain24h, _ := extractAmedasFloat(obs.Precipitation24h)

		if !alwaysInclude[id] && rain1h <= 0 && rain24h <= 0 {
			continue
		}

		status := classifyRainRiskStatus(rain1h, rain24h)

		pt := store.RiverTimeSeriesPoint{
			StationID:     meta.ID,
			StationName:   meta.Name,
			RiverName:     "AMeDAS 雨量",
			Timestamp:     obsAt.UTC().Format("200601021504"),
			ObservedAt:    obsAt.UTC(),
			Latitude:      meta.Latitude,
			Longitude:     meta.Longitude,
			WaterLevelM:   rain1h,
			WarningLevelM: warnRainMmH,
			DangerLevelM:  dangerRainMmH,
			Status:        status,
		}
		points = append(points, pt)

		if status == "danger" || status == "warning" {
			sev := "warning"
			lvl := "激しい雨 (浸水・増水警戒)"
			if status == "danger" {
				sev = "critical"
				lvl = "非常に激しい雨 (氾濫・土砂危険)"
			}
			alerts = append(alerts, store.DisasterRecord{
				ID:         fmt.Sprintf("amedas-%s-%s", meta.ID, obsAt.UTC().Format("2006010215")),
				Source:     "mlit_river",
				Category:   "flood",
				Severity:   sev,
				AlertLevel: lvl,
				Title:      fmt.Sprintf("%s: %.1f mm/h (%s)", meta.Name, rain1h, lvl),
				Detail:     fmt.Sprintf("1時間降水量 %.1f mm / 24時間降水量 %.1f mm", rain1h, rain24h),
				AreaName:   meta.Name,
				Latitude:   meta.Latitude,
				Longitude:  meta.Longitude,
				StartedAt:  obsAt.UTC(),
				UpdatedAt:  obsAt.UTC(),
				IsActive:   true,
			})
		}
	}
	return points, alerts, nil
}

func extractAmedasFloat(arr []any) (float64, bool) {
	if len(arr) < 2 || arr[0] == nil {
		return 0, false
	}
	// arr[1] is quality code (0 = normal)
	if q, ok := arr[1].(float64); ok && q != 0 {
		return 0, false
	}
	if v, ok := arr[0].(float64); ok {
		return v, true
	}
	return 0, false
}

func classifyRainRiskStatus(rain1h, rain24h float64) string {
	if rain1h >= 50.0 || rain24h >= 200.0 {
		return "danger"
	}
	if rain1h >= 30.0 || rain24h >= 120.0 {
		return "warning"
	}
	if rain1h >= 10.0 || rain24h >= 50.0 {
		return "caution"
	}
	return "normal"
}
