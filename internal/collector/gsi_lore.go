package collector

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// GsiLoreClient fetches GSI Natural Disaster Monument (自然災害伝承碑) GeoJSON ZIP
// and caches it locally in bbolt to avoid repeated requests.
type GsiLoreClient struct {
	Client          *http.Client
	Limiter         *RateLimiter
	Cache           *HTTPCache
	DownloadPageURL string
	FallbackZipURL  string
}

// NewGsiLoreClient creates a GsiLoreClient pointing to the GSI nationwide disaster lore download page.
func NewGsiLoreClient(rl *RateLimiter, cache *HTTPCache) *GsiLoreClient {
	return &GsiLoreClient{
		Client:          newHTTPClient(45 * time.Second),
		Limiter:         rl,
		Cache:           cache,
		DownloadPageURL: "https://www.gsi.go.jp/bousaichiri/denshouhi_download.html",
		FallbackZipURL:  "https://www.gsi.go.jp/common/000250767.zip",
	}
}

type gsiLoreFC struct {
	Features []struct {
		ID       any `json:"id"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]any `json:"properties"`
	} `json:"features"`
}

var (
	yearRegex        = regexp.MustCompile(`(1[0-9]{3}|20[0-9]{2})`)
	loreGeoJSONHref  = regexp.MustCompile(`href="([^"]+\.zip)"[^>]*>[^<]*GeoJSON`)
)

func (c *GsiLoreClient) resolveZipURL(ctx context.Context) string {
	if c.DownloadPageURL != "" {
		if raw, _, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderGSI, c.Cache, c.DownloadPageURL); err == nil && len(raw) > 0 {
			if m := loreGeoJSONHref.FindSubmatch(raw); len(m) >= 2 {
				href := string(m[1])
				if strings.HasPrefix(href, "http") {
					return href
				}
				if strings.HasPrefix(href, "/") {
					return "https://www.gsi.go.jp" + href
				}
				return "https://www.gsi.go.jp/bousaichiri/" + href
			}
		}
	}
	return c.FallbackZipURL
}

// FetchAllLore downloads the nationwide GSI Natural Disaster Monuments GeoJSON ZIP and extracts it.
func (c *GsiLoreClient) FetchAllLore(ctx context.Context) ([]store.DisasterLoreRecord, bool, error) {
	zipURL := c.resolveZipURL(ctx)
	raw, notModified, err := fetchBytesConditionalWithLimit(ctx, c.Client, c.Limiter, ProviderGSI, c.Cache, zipURL, 64*1024*1024)
	if err != nil {
		return nil, false, err
	}
	if notModified {
		return nil, true, nil
	}

	geojsonBytes, err := extractGeoJSONFromZip(raw)
	if err != nil {
		// If the payload is already plain GeoJSON (e.g. in tests), parse directly.
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			geojsonBytes = raw
		} else {
			return nil, false, err
		}
	}

	records, err := ParseGSILoreGeoJSON(geojsonBytes)
	return records, false, err
}

func extractGeoJSONFromZip(zipBytes []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open gsi lore zip: %w", err)
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".geojson") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(rc, 64*1024*1024))
			_ = rc.Close()
			if readErr != nil {
				return nil, readErr
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("no .geojson file found inside gsi lore zip")
}

// ParseGSILoreGeoJSON parses GSI disaster_lore GeoJSON into DisasterLoreRecords.
func ParseGSILoreGeoJSON(raw []byte) ([]store.DisasterLoreRecord, error) {
	var fc gsiLoreFC
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("decode gsi lore geojson: %w", err)
	}

	out := make([]store.DisasterLoreRecord, 0, len(fc.Features))
	for i, f := range fc.Features {
		lat, lon, _ := extractGeoJSONCoords(f.Geometry.Type, f.Geometry.Coordinates)
		if lat == 0 && lon == 0 {
			continue
		}

		props := f.Properties
		id := propString(props, "ID", "id", "碑ID")
		if id == "" && f.ID != nil {
			id = fmt.Sprintf("%v", f.ID)
		}
		if id == "" {
			id = fmt.Sprintf("lore-%d-%.4f-%.4f", i, lat, lon)
		}

		name := propString(props, "碑名", "name", "title")
		if name == "" {
			name = "自然災害伝承碑"
		}
		kind := propString(props, "災害種別", "disasterKind", "kind")
		yearText := propString(props, "災害名", "被災年", "建立年", "year")
		summary := propString(props, "伝承内容", "災害の概要", "summary", "description")
		pref := propString(props, "都道府県名", "都道府県", "prefecture")
		muni := propString(props, "市区町村名", "市区町村", "所在地", "municipality")

		year := extractYear(yearText + " " + summary)

		out = append(out, store.DisasterLoreRecord{
			ID:           id,
			Name:         name,
			Prefecture:   pref,
			Municipality: muni,
			DisasterKind: kind,
			Category:     normalizeLoreCategory(kind),
			Year:         year,
			YearText:     yearText,
			Summary:      summary,
			Latitude:     lat,
			Longitude:    lon,
		})
	}
	return out, nil
}

func propString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func extractYear(text string) int {
	m := yearRegex.FindString(text)
	if m == "" {
		return 0
	}
	y, err := strconv.Atoi(m)
	if err != nil {
		return 0
	}
	return y
}

func normalizeLoreCategory(kind string) string {
	switch {
	case strings.Contains(kind, "津波"):
		return "tsunami"
	case strings.Contains(kind, "土砂") || strings.Contains(kind, "崖") || strings.Contains(kind, "山津波") || strings.Contains(kind, "地すべり") || strings.Contains(kind, "土石流"):
		return "landslide"
	case strings.Contains(kind, "雪崩") || strings.Contains(kind, "なだれ") || strings.Contains(kind, "雪"):
		return "snow"
	case strings.Contains(kind, "洪水") || strings.Contains(kind, "水害") || strings.Contains(kind, "高潮") || strings.Contains(kind, "氾濫") || strings.Contains(kind, "破堤"):
		return "flood"
	default:
		return "other"
	}
}
