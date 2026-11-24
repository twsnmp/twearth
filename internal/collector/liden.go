package collector

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// LidenClient fetches JMA LIDEN lightning observations and counts strikes near home coordinates.
type LidenClient struct {
	Client          *http.Client
	Limiter         *RateLimiter
	TargetTimesURL  string
	GeoJSONTemplate string
}

// NewLidenClient creates a LidenClient with official JMA Bosai endpoints.
func NewLidenClient() *LidenClient {
	return &LidenClient{
		Client:          newHTTPClient(25 * time.Second),
		TargetTimesURL:  "https://www.jma.go.jp/bosai/jmatile/data/nowc/targetTimes_N3.json",
		GeoJSONTemplate: "https://www.jma.go.jp/bosai/jmatile/data/nowc/%s/none/%s/surf/liden/data.geojson?id=liden",
	}
}

type lidenTargetTime struct {
	BaseTime  string   `json:"basetime"`
	ValidTime string   `json:"validtime"`
	Elements  []string `json:"elements"`
}

func containsElement(elements []string, target string) bool {
	for _, el := range elements {
		if el == target {
			return true
		}
	}
	return false
}

func selectLatestLidenSlot(times []lidenTargetTime) (lidenTargetTime, bool) {
	for _, t := range times {
		if containsElement(t.Elements, "liden") {
			return t, true
		}
	}
	// Fallback for fixtures or payloads that omit the elements array.
	for _, t := range times {
		if len(t.Elements) == 0 {
			return t, true
		}
	}
	return lidenTargetTime{}, false
}

type lidenGeoJSON struct {
	Features []struct {
		Geometry struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Type any `json:"type"`
		} `json:"properties"`
	} `json:"features"`
}

// FetchLiden retrieves the latest LIDEN GeoJSON and counts strikes within radiusKm of (homeLat, homeLon).
func (c *LidenClient) FetchLiden(ctx context.Context, homeLat, homeLon, radiusKm float64) (LidenSnapshot, error) {
	var times []lidenTargetTime
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderJMA, c.TargetTimesURL, &times); err != nil {
		return LidenSnapshot{}, err
	}
	if len(times) == 0 {
		return LidenSnapshot{RadiusKm: radiusKm}, nil
	}

	latest, ok := selectLatestLidenSlot(times)
	if !ok {
		return LidenSnapshot{RadiusKm: radiusKm}, nil
	}
	url := fmt.Sprintf(c.GeoJSONTemplate, latest.BaseTime, latest.ValidTime)
	var geo lidenGeoJSON
	if err := fetchJSONLimited(ctx, c.Client, c.Limiter, ProviderJMA, url, &geo); err != nil {
		return LidenSnapshot{}, err
	}

	strikes := make([]LightningStrike, 0, len(geo.Features))
	nearby := 0
	for _, f := range geo.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		lon := f.Geometry.Coordinates[0]
		lat := f.Geometry.Coordinates[1]
		dist := HaversineKm(homeLat, homeLon, lat, lon)
		if dist <= radiusKm {
			nearby++
		}
		strikes = append(strikes, LightningStrike{
			Latitude:   lat,
			Longitude:  lon,
			Type:       fmt.Sprintf("%v", f.Properties.Type),
			DistanceKm: dist,
		})
	}

	return LidenSnapshot{
		ValidTime:   latest.ValidTime,
		TotalCount:  len(strikes),
		NearbyCount: nearby,
		RadiusKm:    radiusKm,
		Strikes:     strikes,
	}, nil
}
