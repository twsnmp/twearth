package collector

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// JmaExtraClient fetches JMA Bosai XML "extra" Atom feed (warnings, landslide, flood, avalanche).
type JmaExtraClient struct {
	Client       *http.Client
	Limiter      *RateLimiter
	Cache        *HTTPCache
	ExtraURL     string
	ExtraLongURL string
}

// NewJmaExtraClient creates a JmaExtraClient with official JMA XML feed URLs.
func NewJmaExtraClient(rl *RateLimiter, cache *HTTPCache) *JmaExtraClient {
	return &JmaExtraClient{
		Client:       newHTTPClient(20 * time.Second),
		Limiter:      rl,
		Cache:        cache,
		ExtraURL:     "https://www.data.jma.go.jp/developer/xml/feed/extra.xml",
		ExtraLongURL: "https://www.data.jma.go.jp/developer/xml/feed/extra_l.xml",
	}
}

type jmaAtomFeed struct {
	Entries []jmaAtomEntry `xml:"entry"`
}

type jmaAtomEntry struct {
	ID      string `xml:"id"`
	Title   string `xml:"title"`
	Updated string `xml:"updated"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Link struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Content struct {
		Text string `xml:",chardata"`
	} `xml:"content"`
}

// FetchExtraFeed fetches and parses JMA extra.xml (or extra_l.xml on initial backfill).
// It returns only entries not present in seenIDs, along with the updated seen list.
func (c *JmaExtraClient) FetchExtraFeed(ctx context.Context, useLong bool, seenIDs []string) ([]store.DisasterRecord, []string, bool, error) {
	targetURL := c.ExtraURL
	if useLong && c.ExtraLongURL != "" {
		targetURL = c.ExtraLongURL
	}
	raw, notModified, err := fetchBytesConditional(ctx, c.Client, c.Limiter, ProviderJMA, c.Cache, targetURL)
	if err != nil {
		return nil, seenIDs, false, err
	}
	if notModified {
		return nil, seenIDs, true, nil
	}
	records, nextSeen, err := ParseJmaExtraFeed(raw, seenIDs)
	return records, nextSeen, false, err
}

// ParseJmaExtraFeed parses JMA Atom XML bytes into normalized DisasterRecords.
func ParseJmaExtraFeed(raw []byte, seenIDs []string) ([]store.DisasterRecord, []string, error) {
	var feed jmaAtomFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, seenIDs, err
	}

	seenSet := make(map[string]struct{}, len(seenIDs))
	for _, id := range seenIDs {
		seenSet[id] = struct{}{}
	}

	var out []store.DisasterRecord
	nextSeen := append([]string(nil), seenIDs...)

	for _, e := range feed.Entries {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			continue
		}
		if _, already := seenSet[id]; already {
			continue
		}
		seenSet[id] = struct{}{}
		nextSeen = append(nextSeen, id)

		cat, sev, alertLvl, ok := classifyJmaExtraEntry(e.Title, e.Content.Text)
		if !ok {
			continue
		}

		ts := parseAtomTime(e.Updated)
		area := strings.TrimSpace(e.Author.Name)
		lat, lon := lookupJmaOfficeCoord(area, e.Title+" "+e.Content.Text)

		title := strings.TrimSpace(e.Title)
		if area != "" && !strings.Contains(title, area) {
			title = title + " (" + area + ")"
		}

		out = append(out, store.DisasterRecord{
			ID:         sanitizeFeedID(id),
			Source:     "jma_extra",
			Category:   cat,
			Severity:   sev,
			AlertLevel: alertLvl,
			Title:      title,
			Detail:     strings.TrimSpace(e.Content.Text),
			AreaName:   area,
			Latitude:   lat,
			Longitude:  lon,
			StartedAt:  ts,
			UpdatedAt:  ts,
			IsActive:   time.Since(ts) < 24*time.Hour,
			URL:        strings.TrimSpace(e.Link.Href),
		})
	}

	if len(nextSeen) > 400 {
		nextSeen = nextSeen[len(nextSeen)-400:]
	}
	return out, nextSeen, nil
}

func classifyJmaExtraEntry(title, content string) (category, severity, alertLevel string, keep bool) {
	combined := title + " " + content

	switch {
	case strings.Contains(title, "土砂災害警戒情報"):
		return "landslide", "critical", "土砂災害警戒情報", true
	case strings.Contains(title, "指定河川洪水予報"):
		sev := "warning"
		lvl := "氾濫注意/警戒"
		if strings.Contains(combined, "氾濫発生") {
			sev = "critical"
			lvl = "氾濫発生"
		} else if strings.Contains(combined, "氾濫危険") {
			sev = "critical"
			lvl = "氾濫危険"
		}
		return "flood", sev, lvl, true
	case strings.Contains(title, "特別警報"):
		return "warning", "critical", "特別警報", true
	case strings.Contains(title, "気象警報・注意報") || strings.Contains(title, "警報"):
		if strings.Contains(combined, "解除") && !strings.Contains(combined, "警報") {
			return "", "", "", false
		}
		cat := "warning"
		if strings.Contains(combined, "洪水") || strings.Contains(combined, "浸水") {
			cat = "flood"
		} else if strings.Contains(combined, "土砂") {
			cat = "landslide"
		} else if strings.Contains(combined, "なだれ") || strings.Contains(combined, "大雪") {
			cat = "snow"
		}
		sev := "info"
		lvl := "注意報"
		if strings.Contains(combined, "警報") {
			sev = "warning"
			lvl = "警報"
		}
		return cat, sev, lvl, true
	case strings.Contains(combined, "なだれ"):
		return "snow", "warning", "なだれ注意報", true
	default:
		return "", "", "", false
	}
}

func parseAtomTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

func sanitizeFeedID(id string) string {
	id = strings.TrimPrefix(id, "urn:uuid:")
	replacer := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return replacer.Replace(id)
}

var jmaPrefCoords = []struct {
	key string
	lat float64
	lon float64
}{
	{"札幌", 43.062, 141.354}, {"北海道", 43.062, 141.354},
	{"青森", 40.824, 140.740}, {"盛岡", 39.702, 141.154}, {"岩手", 39.702, 141.154},
	{"仙台", 38.268, 140.869}, {"宮城", 38.268, 140.869}, {"秋田", 39.719, 140.102},
	{"山形", 38.255, 140.339}, {"福島", 37.761, 140.474}, {"水戸", 36.366, 140.471}, {"茨城", 36.366, 140.471},
	{"宇都宮", 36.555, 139.883}, {"栃木", 36.555, 139.883}, {"前橋", 36.389, 139.063}, {"群馬", 36.389, 139.063},
	{"熊谷", 36.147, 139.389}, {"埼玉", 35.861, 139.645}, {"銚子", 35.734, 140.827}, {"千葉", 35.607, 140.106},
	{"東京", 35.689, 139.692}, {"横浜", 35.444, 139.638}, {"神奈川", 35.444, 139.638},
	{"新潟", 37.916, 139.036}, {"富山", 36.696, 137.213}, {"金沢", 36.561, 136.656}, {"石川", 36.561, 136.656},
	{"福井", 36.064, 136.219}, {"甲府", 35.662, 138.568}, {"山梨", 35.662, 138.568},
	{"長野", 36.648, 138.195}, {"岐阜", 35.423, 136.760}, {"静岡", 34.976, 138.383},
	{"名古屋", 35.181, 136.906}, {"愛知", 35.181, 136.906}, {"津", 34.718, 136.506}, {"三重", 34.718, 136.506},
	{"彦根", 35.274, 136.259}, {"滋賀", 35.004, 135.869}, {"京都", 35.012, 135.768},
	{"大阪", 34.693, 135.502}, {"神戸", 34.690, 135.195}, {"兵庫", 34.690, 135.195},
	{"奈良", 34.685, 135.805}, {"和歌山", 34.230, 135.171}, {"鳥取", 35.501, 134.235},
	{"松江", 35.468, 133.048}, {"島根", 35.468, 133.048}, {"岡山", 34.655, 133.919},
	{"広島", 34.385, 132.455}, {"下関", 33.958, 130.941}, {"山口", 34.178, 131.474},
	{"徳島", 34.070, 134.555}, {"高松", 34.343, 134.047}, {"香川", 34.343, 134.047},
	{"松山", 33.839, 132.765}, {"愛媛", 33.839, 132.765}, {"高知", 33.560, 133.531},
	{"福岡", 33.590, 130.402}, {"佐賀", 33.263, 130.301}, {"長崎", 32.750, 129.878},
	{"熊本", 32.803, 130.708}, {"大分", 33.238, 131.613}, {"宮崎", 31.908, 131.420},
	{"鹿児島", 31.596, 130.557}, {"沖縄", 26.212, 127.679}, {"那覇", 26.212, 127.679},
	{"石垣", 24.340, 124.155}, {"宮古", 24.805, 125.281},
}

func lookupJmaOfficeCoord(area, text string) (float64, float64) {
	haystack := area + " " + text
	for _, p := range jmaPrefCoords {
		if strings.Contains(haystack, p.key) {
			return p.lat, p.lon
		}
	}
	return 35.689, 139.692 // fallback to Tokyo
}
