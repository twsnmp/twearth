package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// KikikuruClient fetches JMA Kikikuru (危険度分布) target times and builds XYZ raster tile URLs.
type KikikuruClient struct {
	Client          *http.Client
	Limiter         *RateLimiter
	Cache           *HTTPCache
	TargetTimesURL  string
	TileURLTemplate string
}

// NewKikikuruClient creates a KikikuruClient with official JMA Bosai endpoints.
func NewKikikuruClient(rl *RateLimiter, cache *HTTPCache) *KikikuruClient {
	return &KikikuruClient{
		Client:          newHTTPClient(15 * time.Second),
		Limiter:         rl,
		Cache:           cache,
		TargetTimesURL:  "https://www.jma.go.jp/bosai/jmatile/data/risk/targetTimes.json",
		TileURLTemplate: "https://www.jma.go.jp/bosai/jmatile/data/risk/%s/%s/%s/surf/%s/{z}/{x}/{y}.png",
	}
}

type kikikuruTargetTime struct {
	BaseTime  string `json:"basetime"`
	ValidTime string `json:"validtime"`
	Member    string `json:"member"`
}

// FetchKikikuru retrieves the latest Kikikuru tile URLs for land, inund, and flood layers.
func (c *KikikuruClient) FetchKikikuru(ctx context.Context) (*KikikuruInfo, bool, error) {
	raw, notModified, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderJMA, c.Cache, c.TargetTimesURL)
	if err != nil {
		return nil, false, err
	}
	if notModified {
		return nil, true, nil
	}

	var times []kikikuruTargetTime
	if err := json.Unmarshal(raw, &times); err != nil {
		return nil, false, fmt.Errorf("decode kikikuru targetTimes: %w", err)
	}
	if len(times) == 0 {
		return nil, false, nil
	}

	latest := times[0]
	member := latest.Member
	if member == "" {
		member = "immed0"
	}
	info := &KikikuruInfo{
		Land: &KikikuruLayerTile{
			Kind:            "land",
			BaseTime:        latest.BaseTime,
			ValidTime:       latest.ValidTime,
			TileURLTemplate: fmt.Sprintf(c.TileURLTemplate, latest.BaseTime, member, latest.ValidTime, "land"),
		},
		Inund: &KikikuruLayerTile{
			Kind:            "inund",
			BaseTime:        latest.BaseTime,
			ValidTime:       latest.ValidTime,
			TileURLTemplate: fmt.Sprintf(c.TileURLTemplate, latest.BaseTime, member, latest.ValidTime, "inund"),
		},
		Flood: &KikikuruLayerTile{
			Kind:            "flood",
			BaseTime:        latest.BaseTime,
			ValidTime:       latest.ValidTime,
			TileURLTemplate: fmt.Sprintf(c.TileURLTemplate, latest.BaseTime, member, latest.ValidTime, "flood"),
		},
	}
	return info, false, nil
}
