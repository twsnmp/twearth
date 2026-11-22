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

func extractJmaActiveBody(title, content string) string {
	body := strings.TrimSpace(content)
	if idx := strings.Index(body, "】"); idx >= 0 {
		body = strings.TrimSpace(body[idx+len("】"):])
	}
	if body == "" {
		// Fallback when test fixtures or minimal feeds omit <content>.
		if !strings.Contains(title, "気象特別警報・警報・注意報") && !strings.Contains(title, "気象警報・注意報") {
			return strings.TrimSpace(title)
		}
		return ""
	}

	// Normalize separators and evaluate sentence by sentence so cancelled clauses don't trigger alerts.
	normalized := strings.NewReplacer("\r\n", "\n", "。", "\n").Replace(body)
	lines := strings.Split(normalized, "\n")
	var active []string
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		// Handle transition clauses like "特別警報を警報に切り替えました" or "警報を解除し、注意報に切り替え".
		if idx := strings.Index(line, "解除し、"); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len("解除し、"):])
			if rest != "" && !strings.Contains(rest, "解除") {
				active = append(active, rest)
			}
			continue
		}
		if strings.Contains(line, "に切り替え") {
			if idx := strings.Index(line, "を"); idx >= 0 {
				rest := strings.TrimSpace(line[idx+len("を"):])
				if rest != "" {
					active = append(active, rest)
					continue
				}
			}
		}
		if strings.Contains(line, "解除") {
			continue
		}
		active = append(active, line)
	}
	return strings.Join(active, " ")
}

func classifyJmaExtraEntry(title, content string) (category, severity, alertLevel string, keep bool) {
	activeBody := extractJmaActiveBody(title, content)
	if activeBody == "" {
		return "", "", "", false
	}

	switch {
	case strings.Contains(title, "土砂災害警戒情報"):
		return "landslide", "critical", "土砂災害警戒情報", true
	case strings.Contains(title, "記録的短時間大雨情報") || strings.Contains(title, "顕著な大雨"):
		return "flood", "critical", "記録的短時間大雨情報", true
	case strings.Contains(title, "指定河川洪水予報"):
		sev := "info"
		lvl := "氾濫注意"
		switch {
		case strings.Contains(activeBody, "氾濫発生") || strings.Contains(activeBody, "はん濫発生"):
			sev = "critical"
			lvl = "氾濫発生"
		case strings.Contains(activeBody, "氾濫危険") || strings.Contains(activeBody, "はん濫危険"):
			sev = "critical"
			lvl = "氾濫危険"
		case strings.Contains(activeBody, "氾濫警戒") || strings.Contains(activeBody, "はん濫警戒"):
			sev = "warning"
			lvl = "氾濫警戒"
		}
		return "flood", sev, lvl, true
	case strings.Contains(title, "竜巻注意情報"):
		return "warning", "warning", "竜巻注意情報", true
	case strings.Contains(title, "気象特別警報") || strings.Contains(title, "気象警報") || strings.Contains(title, "特別警報") || strings.Contains(title, "警報") || strings.Contains(title, "注意報"):
		cat := "warning"
		switch {
		case strings.Contains(activeBody, "洪水") || strings.Contains(activeBody, "浸水") || strings.Contains(activeBody, "増水"):
			cat = "flood"
		case strings.Contains(activeBody, "土砂"):
			cat = "landslide"
		case strings.Contains(activeBody, "なだれ") || strings.Contains(activeBody, "大雪") || strings.Contains(activeBody, "着雪"):
			cat = "snow"
		}
		sev := "info"
		lvl := "注意報"
		switch {
		case strings.Contains(activeBody, "特別警報"):
			sev = "critical"
			lvl = "特別警報"
		case strings.Contains(activeBody, "警報") || strings.Contains(activeBody, "警戒してください") || strings.Contains(activeBody, "警戒し"):
			sev = "warning"
			lvl = "警報"
		}
		return cat, sev, lvl, true
	case strings.Contains(activeBody, "なだれ"):
		return "snow", "info", "なだれ注意報", true
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
	// Specific islands & regional meteorological offices first (before broad prefecture names)
	{"石垣", 24.340, 124.155}, {"八重山", 24.340, 124.155},
	{"宮古", 24.805, 125.281}, {"南大東", 25.829, 131.228},
	{"那覇", 26.212, 127.679}, {"沖縄", 26.212, 127.679},
	{"名瀬", 28.377, 129.494}, {"奄美", 28.377, 129.494},
	{"種子島", 30.727, 130.996}, {"小笠原", 27.094, 142.192}, {"父島", 27.094, 142.192},
	{"函館", 41.769, 140.729}, {"室蘭", 42.315, 140.974},
	{"旭川", 43.771, 142.365}, {"稚内", 45.416, 141.673},
	{"網走", 44.021, 144.273}, {"帯広", 42.924, 143.196},
	{"釧路", 42.985, 144.381}, {"札幌", 43.062, 141.354}, {"北海道", 43.062, 141.354},
	{"青森", 40.824, 140.740}, {"盛岡", 39.702, 141.154}, {"岩手", 39.702, 141.154},
	{"仙台", 38.268, 140.869}, {"宮城", 38.268, 140.869}, {"秋田", 39.719, 140.102},
	{"山形", 38.255, 140.339}, {"福島", 37.761, 140.474}, {"水戸", 36.366, 140.471}, {"茨城", 36.366, 140.471},
	{"宇都宮", 36.555, 139.883}, {"栃木", 36.555, 139.883}, {"前橋", 36.389, 139.063}, {"群馬", 36.389, 139.063},
	{"熊谷", 36.147, 139.389}, {"埼玉", 35.861, 139.645}, {"銚子", 35.734, 140.827}, {"千葉", 35.607, 140.106},
	{"東京", 35.689, 139.692}, {"横浜", 35.444, 139.638}, {"神奈川", 35.444, 139.638},
	{"新潟", 37.916, 139.036}, {"富山", 36.696, 137.213}, {"金沢", 36.561, 136.656}, {"石川", 36.561, 136.656},
	{"福井", 36.064, 136.219}, {"甲府", 35.662, 138.568}, {"山梨", 35.662, 138.568},
	{"長野", 36.648, 138.195}, {"飯田", 35.515, 137.822}, {"高山", 36.146, 137.252}, {"岐阜", 35.423, 136.760},
	{"浜松", 34.711, 137.726}, {"静岡", 34.976, 138.383},
	{"名古屋", 35.181, 136.906}, {"愛知", 35.181, 136.906}, {"津", 34.718, 136.506}, {"三重", 34.718, 136.506},
	{"彦根", 35.274, 136.259}, {"滋賀", 35.004, 135.869}, {"舞鶴", 35.475, 135.386}, {"京都", 35.012, 135.768},
	{"大阪", 34.693, 135.502}, {"神戸", 34.690, 135.195}, {"兵庫", 34.690, 135.195},
	{"奈良", 34.685, 135.805}, {"和歌山", 34.230, 135.171}, {"鳥取", 35.501, 134.235},
	{"松江", 35.468, 133.048}, {"島根", 35.468, 133.048}, {"岡山", 34.655, 133.919},
	{"広島", 34.385, 132.455}, {"下関", 33.958, 130.941}, {"山口", 34.178, 131.474},
	{"徳島", 34.070, 134.555}, {"高松", 34.343, 134.047}, {"香川", 34.343, 134.047},
	{"松山", 33.839, 132.765}, {"愛媛", 33.839, 132.765}, {"高知", 33.560, 133.531},
	{"福岡", 33.590, 130.402}, {"佐賀", 33.263, 130.301}, {"長崎", 32.750, 129.878},
	{"熊本", 32.803, 130.708}, {"大分", 33.238, 131.613}, {"宮崎", 31.908, 131.420},
	{"鹿児島", 31.596, 130.557},
}

func lookupJmaOfficeCoord(area, text string) (float64, float64) {
	// Prefer author/office name first so sub-regional offices (e.g. 名瀬, 石垣) aren't shadowed by prefecture names in content.
	for _, p := range jmaPrefCoords {
		if area != "" && strings.Contains(area, p.key) {
			return p.lat, p.lon
		}
	}
	for _, p := range jmaPrefCoords {
		if strings.Contains(text, p.key) {
			return p.lat, p.lon
		}
	}
	return 35.689, 139.692 // fallback to Tokyo
}
