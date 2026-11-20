package collector

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// RadarClient fetches JMA High-Resolution Precipitation Nowcast target times and builds raster tile URLs.
type RadarClient struct {
	Client          *http.Client
	TargetTimesURL  string
	TileURLTemplate string
}

// NewRadarClient creates a RadarClient with official JMA Bosai endpoints.
func NewRadarClient() *RadarClient {
	return &RadarClient{
		Client:          newHTTPClient(15 * time.Second),
		TargetTimesURL:  "https://www.jma.go.jp/bosai/jmatile/data/nowc/targetTimes_N1.json",
		TileURLTemplate: "https://www.jma.go.jp/bosai/jmatile/data/nowc/%s/none/%s/surf/hrpns/{z}/{x}/{y}.png",
	}
}

type radarTargetTime struct {
	BaseTime  string `json:"basetime"`
	ValidTime string `json:"validtime"`
}

// FetchRadar retrieves the latest precipitation nowcast tile metadata.
func (c *RadarClient) FetchRadar(ctx context.Context) (RadarTileInfo, error) {
	var times []radarTargetTime
	if err := fetchJSON(ctx, c.Client, c.TargetTimesURL, &times); err != nil {
		return RadarTileInfo{}, err
	}
	if len(times) == 0 {
		return RadarTileInfo{}, nil
	}

	latest := times[0]
	return RadarTileInfo{
		BaseTime:        latest.BaseTime,
		ValidTime:       latest.ValidTime,
		TileURLTemplate: fmt.Sprintf(c.TileURLTemplate, latest.BaseTime, latest.ValidTime),
	}, nil
}
