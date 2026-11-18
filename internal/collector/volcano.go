package collector

import (
	"context"
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// VolcanoClient fetches JMA active volcano list, alert levels, and eqvol XML feed.
type VolcanoClient struct {
	Client      *http.Client
	ListURL     string
	WarningURL  string
	EqVolXMLURL string
}

// NewVolcanoClient creates a VolcanoClient with official JMA endpoints.
func NewVolcanoClient() *VolcanoClient {
	return &VolcanoClient{
		Client:      newHTTPClient(20 * time.Second),
		ListURL:     "https://www.jma.go.jp/bosai/volcano/const/volcano_list.json",
		WarningURL:  "https://www.jma.go.jp/bosai/volcano/data/warning.json",
		EqVolXMLURL: "https://www.data.jma.go.jp/developer/xml/feed/eqvol.xml",
	}
}

type jmaVolcanoItem struct {
	Code    string    `json:"code"`
	NameJp  string    `json:"name_jp"`
	LatLon  []float64 `json:"latlon"`
}

type jmaVolcanoWarning map[string]struct {
	Level     string `json:"level"`
	Condition string `json:"condition"`
}

type eqVolFeed struct {
	Entries []struct {
		ID      string `xml:"id"`
		Title   string `xml:"title"`
		Updated string `xml:"updated"`
		Content string `xml:"content"`
	} `xml:"entry"`
}

// FetchVolcanoes retrieves active volcanoes merged with current warning levels.
func (c *VolcanoClient) FetchVolcanoes(ctx context.Context) ([]VolcanoInfo, error) {
	var list []jmaVolcanoItem
	if err := fetchJSON(ctx, c.Client, c.ListURL, &list); err != nil {
		return nil, err
	}

	var warnings jmaVolcanoWarning
	_ = fetchJSON(ctx, c.Client, c.WarningURL, &warnings)

	out := make([]VolcanoInfo, 0, len(list))
	for _, v := range list {
		if len(v.LatLon) < 2 {
			continue
		}
		info := VolcanoInfo{
			Code:       v.Code,
			Name:       v.NameJp,
			Latitude:   v.LatLon[0],
			Longitude:  v.LatLon[1],
			AlertLevel: 1,
		}
		if w, ok := warnings[v.Code]; ok {
			if lvl, err := strconv.Atoi(strings.TrimSpace(w.Level)); err == nil && lvl > 0 {
				info.AlertLevel = lvl
			}
			info.WarningTitle = w.Condition
		}
		out = append(out, info)
	}
	return out, nil
}

// FetchEqVolFeedEntries retrieves recent volcano/earthquake Atom entries from JMA XML feed.
func (c *VolcanoClient) FetchEqVolFeedEntries(ctx context.Context) ([]string, error) {
	raw, err := fetchBytes(ctx, c.Client, c.EqVolXMLURL)
	if err != nil {
		return nil, err
	}
	var feed eqVolFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, err
	}
	var titles []string
	for _, e := range feed.Entries {
		if strings.Contains(e.Title, "噴火") || strings.Contains(e.Title, "火山") {
			titles = append(titles, e.Title)
		}
	}
	return titles, nil
}
