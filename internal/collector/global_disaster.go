package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// GlobalDisasterClient fetches global disaster alerts from GDACS and NASA EONET v3.
type GlobalDisasterClient struct {
	Client   *http.Client
	Limiter  *RateLimiter
	Cache    *HTTPCache
	GDACSURL string
	EONETURL string
}

// NewGlobalDisasterClient creates a GlobalDisasterClient with official GDACS and NASA EONET endpoints.
func NewGlobalDisasterClient(rl *RateLimiter, cache *HTTPCache) *GlobalDisasterClient {
	return &GlobalDisasterClient{
		Client:   newHTTPClient(25 * time.Second),
		Limiter:  rl,
		Cache:    cache,
		GDACSURL: "https://www.gdacs.org/gdacsapi/api/events/geteventlist/SEARCH",
		EONETURL: "https://eonet.gsfc.nasa.gov/api/v3/events?status=open&limit=100",
	}
}

type gdacsFeatureCollection struct {
	Features []gdacsFeature `json:"features"`
}

type gdacsSeverityData struct {
	Severity     float64 `json:"severity,omitempty"`
	SeverityText string  `json:"severitytext,omitempty"`
	SeverityUnit string  `json:"severityunit,omitempty"`
}

type gdacsFeature struct {
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		EventID    any                `json:"eventid"`
		EventType  string             `json:"eventtype"`
		Name       string             `json:"name"`
		EventName  string             `json:"eventname"`
		Desc       string             `json:"description"`
		HTMLDesc   string             `json:"htmldescription"`
		AlertLevel string             `json:"alertlevel"`
		Country    string             `json:"country"`
		FromDate   string             `json:"fromdate"`
		ToDate     string             `json:"todate"`
		IsCurrent  string             `json:"iscurrent"`
		URL        any                `json:"url"`
		Population int64              `json:"population"`
		Severity   *gdacsSeverityData `json:"severitydata,omitempty"`
	} `json:"properties"`
}

// FetchGDACS retrieves current global disasters from GDACS GeoJSON API.
func (c *GlobalDisasterClient) FetchGDACS(ctx context.Context) ([]store.DisasterRecord, bool, error) {
	raw, notModified, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderGDACS, c.Cache, c.GDACSURL)
	if err != nil {
		return nil, false, err
	}
	if notModified {
		return nil, true, nil
	}
	records, err := ParseGDACSGeoJSON(raw)
	return records, false, err
}

// ParseGDACSGeoJSON parses GDACS GeoJSON response bytes into DisasterRecords.
func ParseGDACSGeoJSON(raw []byte) ([]store.DisasterRecord, error) {
	var fc gdacsFeatureCollection
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("decode gdacs geojson: %w", err)
	}

	out := make([]store.DisasterRecord, 0, len(fc.Features))
	for _, f := range fc.Features {
		id := fmt.Sprintf("%v", f.Properties.EventID)
		if id == "" || id == "<nil>" {
			continue
		}
		lat, lon, poly := extractGeoJSONCoords(f.Geometry.Type, f.Geometry.Coordinates)
		if lat == 0 && lon == 0 && len(poly) == 0 {
			continue
		}

		cat := mapGDACSCategory(f.Properties.EventType)
		sev := mapGDACSSeverity(f.Properties.AlertLevel)
		title := strings.TrimSpace(f.Properties.Name)
		if title == "" {
			title = strings.TrimSpace(f.Properties.EventName)
		}
		if title == "" {
			title = fmt.Sprintf("GDACS %s #%s", f.Properties.EventType, id)
		}

		started := parseFlexibleTime(f.Properties.FromDate)
		updated := parseFlexibleTime(f.Properties.ToDate)
		if updated.IsZero() {
			updated = started
		}

		var linkURL string
		if s, ok := f.Properties.URL.(string); ok {
			linkURL = s
		} else if m, ok := f.Properties.URL.(map[string]any); ok {
			if r, ok := m["report"].(string); ok {
				linkURL = r
			}
		}

		isActive := strings.EqualFold(f.Properties.IsCurrent, "true") || time.Since(updated) < 72*time.Hour

		out = append(out, store.DisasterRecord{
			ID:                f.Properties.EventType + "-" + id,
			Source:            "gdacs",
			Category:          cat,
			Severity:          sev,
			AlertLevel:        f.Properties.AlertLevel,
			Title:             title,
			Detail:            strings.TrimSpace(f.Properties.Desc),
			AreaName:          strings.TrimSpace(f.Properties.Country),
			Latitude:          lat,
			Longitude:         lon,
			Polygon:           poly,
			PopulationExposed: f.Properties.Population,
			StartedAt:         started,
			UpdatedAt:         updated,
			IsActive:          isActive,
			URL:               linkURL,
		})
	}
	return out, nil
}

type eonetResponse struct {
	Events []struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Link       string `json:"link"`
		Closed     string `json:"closed"`
		Categories []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"categories"`
		Geometry []struct {
			Date        string          `json:"date"`
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"events"`
}

// FetchEONET retrieves open natural events from NASA EONET v3 API.
func (c *GlobalDisasterClient) FetchEONET(ctx context.Context) ([]store.DisasterRecord, bool, error) {
	raw, notModified, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderEONET, c.Cache, c.EONETURL)
	if err != nil {
		return nil, false, err
	}
	if notModified {
		return nil, true, nil
	}
	records, err := ParseEONETJSON(raw)
	return records, false, err
}

// ParseEONETJSON parses NASA EONET v3 JSON response into DisasterRecords.
func ParseEONETJSON(raw []byte) ([]store.DisasterRecord, error) {
	var resp eonetResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode eonet json: %w", err)
	}

	out := make([]store.DisasterRecord, 0, len(resp.Events))
	for _, ev := range resp.Events {
		if ev.ID == "" || len(ev.Geometry) == 0 {
			continue
		}
		cat := "other"
		var catTitles []string
		for _, c := range ev.Categories {
			catTitles = append(catTitles, c.Title)
			mapped := mapEONETCategory(c.ID)
			if mapped != "other" {
				cat = mapped
			}
		}

		// Use the latest geometry point as primary coordinates, and collect track points if multiple.
		var track [][]float64
		var lat, lon float64
		var started, updated time.Time
		for i, g := range ev.Geometry {
			t := parseFlexibleTime(g.Date)
			if i == 0 || t.Before(started) {
				started = t
			}
			if t.After(updated) {
				updated = t
			}
			gLat, gLon, _ := extractGeoJSONCoords(g.Type, g.Coordinates)
			if gLat != 0 || gLon != 0 {
				lat, lon = gLat, gLon
				track = append(track, []float64{gLon, gLat})
			}
		}
		if lat == 0 && lon == 0 {
			continue
		}

		sev := "info"
		if cat == "flood" || cat == "landslide" || cat == "storm" {
			sev = "warning"
		}

		var poly [][]float64
		if len(track) > 1 {
			poly = track
		}

		out = append(out, store.DisasterRecord{
			ID:         ev.ID,
			Source:     "eonet",
			Category:   cat,
			Severity:   sev,
			AlertLevel: strings.Join(catTitles, ", "),
			Title:      strings.TrimSpace(ev.Title),
			Detail:     strings.Join(catTitles, ", "),
			Latitude:   lat,
			Longitude:  lon,
			Polygon:    poly,
			StartedAt:  started,
			UpdatedAt:  updated,
			IsActive:   ev.Closed == "",
			URL:        ev.Link,
		})
	}
	return out, nil
}

func mapGDACSCategory(et string) string {
	switch strings.ToUpper(strings.TrimSpace(et)) {
	case "FL":
		return "flood"
	case "LS":
		return "landslide"
	case "TC":
		return "storm"
	case "WF":
		return "wildfire"
	case "VO":
		return "volcano"
	case "EQ":
		return "quake"
	default:
		return "other"
	}
}

func mapGDACSSeverity(lvl string) string {
	switch strings.ToLower(strings.TrimSpace(lvl)) {
	case "red":
		return "critical"
	case "orange":
		return "warning"
	default:
		return "info"
	}
}

func mapEONETCategory(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "floods":
		return "flood"
	case "landslides":
		return "landslide"
	case "snow":
		return "snow"
	case "wildfires":
		return "wildfire"
	case "severestorms":
		return "storm"
	case "volcanoes":
		return "volcano"
	case "earthquakes":
		return "quake"
	default:
		return "other"
	}
}

func extractGeoJSONCoords(geomType string, raw json.RawMessage) (lat, lon float64, ring [][]float64) {
	if len(raw) == 0 {
		return 0, 0, nil
	}
	switch strings.ToLower(geomType) {
	case "point":
		var pt []float64
		if err := json.Unmarshal(raw, &pt); err == nil && len(pt) >= 2 {
			return pt[1], pt[0], nil
		}
	case "polygon":
		var rings [][][]float64
		if err := json.Unmarshal(raw, &rings); err == nil && len(rings) > 0 && len(rings[0]) > 0 {
			lat, lon = centroidOfRing(rings[0])
			return lat, lon, rings[0]
		}
	case "multipolygon":
		var polys [][][][]float64
		if err := json.Unmarshal(raw, &polys); err == nil && len(polys) > 0 && len(polys[0]) > 0 && len(polys[0][0]) > 0 {
			lat, lon = centroidOfRing(polys[0][0])
			return lat, lon, polys[0][0]
		}
	}
	return 0, 0, nil
}

func centroidOfRing(ring [][]float64) (lat, lon float64) {
	if len(ring) == 0 {
		return 0, 0
	}
	var sumLat, sumLon float64
	var n float64
	for _, pt := range ring {
		if len(pt) >= 2 {
			sumLon += pt[0]
			sumLat += pt[1]
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sumLat / n, sumLon / n
}

func parseFlexibleTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
