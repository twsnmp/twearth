package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// DisasterRecord represents a normalized domestic or global disaster event persisted locally.
type DisasterRecord struct {
	ID                string      `json:"id"`
	Source            string      `json:"source"`   // "jma_extra" | "mlit_river" | "gdacs" | "eonet"
	Category          string      `json:"category"` // "flood" | "landslide" | "snow" | "wildfire" | "storm" | "warning" | "volcano" | "quake" | "other"
	Severity          string      `json:"severity"` // "critical" | "warning" | "info"
	AlertLevel        string      `json:"alertLevel,omitempty"` // e.g. "Red", "Orange", "Green", "氾濫危険", "土砂災害警戒情報"
	Title             string      `json:"title"`
	Detail            string      `json:"detail,omitempty"`
	AreaName          string      `json:"areaName,omitempty"`
	Latitude          float64     `json:"latitude"`
	Longitude         float64     `json:"longitude"`
	Polygon           [][]float64 `json:"polygon,omitempty"` // optional [lon, lat] ring
	PopulationExposed int64       `json:"populationExposed,omitempty"`
	StartedAt         time.Time   `json:"startedAt"`
	UpdatedAt         time.Time   `json:"updatedAt"`
	IsActive          bool        `json:"isActive"`
	URL               string      `json:"url,omitempty"`
}

// RiverTimeSeriesPoint represents a river water level observation keyed by stationId:YYYYMMDDHHmm.
type RiverTimeSeriesPoint struct {
	StationID     string    `json:"stationId"`
	StationName   string    `json:"stationName"`
	RiverName     string    `json:"riverName"`
	Timestamp     string    `json:"timestamp"` // YYYYMMDDHHmm UTC
	ObservedAt    time.Time `json:"observedAt"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	WaterLevelM   float64   `json:"waterLevelM"`
	WarningLevelM float64   `json:"warningLevelM,omitempty"` // 避難判断水位
	DangerLevelM  float64   `json:"dangerLevelM,omitempty"`  // 氾濫危険水位
	Status        string    `json:"status"`                  // "normal" | "caution" | "warning" | "danger"
}

// DisasterLoreRecord represents a GSI Natural Disaster Monument (自然災害伝承碑) entry.
type DisasterLoreRecord struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Prefecture   string  `json:"prefecture,omitempty"`
	Municipality string  `json:"municipality,omitempty"`
	DisasterKind string  `json:"disasterKind"` // e.g. "洪水", "土砂災害", "津波", "雪崩", "高潮"
	Category     string  `json:"category"`     // normalized: "flood" | "landslide" | "snow" | "tsunami" | "other"
	Year         int     `json:"year,omitempty"`
	YearText     string  `json:"yearText,omitempty"`
	Summary      string  `json:"summary,omitempty"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
}

// FeedCursor tracks seen entries and last sync metadata for external feeds.
type FeedCursor struct {
	Key        string    `json:"key"`
	SeenIDs    []string  `json:"seenIds,omitempty"`
	LastSyncAt time.Time `json:"lastSyncAt"`
	ETag       string    `json:"etag,omitempty"`
}

// DisasterHistoryQuery defines parameters for querying and analyzing historical disasters.
type DisasterHistoryQuery struct {
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	RadiusKm    float64   `json:"radiusKm"` // <=0 means global
	FromTime    time.Time `json:"fromTime"`
	ToTime      time.Time `json:"toTime"`
	Categories  []string  `json:"categories,omitempty"`
	Sources     []string  `json:"sources,omitempty"`
	Granularity string    `json:"granularity"` // "auto" | "daily" | "monthly" | "yearly"
}

// DisasterBucketPoint aggregates disaster counts for a time bucket.
type DisasterBucketPoint struct {
	Label         string `json:"label"`
	Count         int    `json:"count"`
	CriticalCount int    `json:"criticalCount"`
	WarningCount  int    `json:"warningCount"`
}

// LoreCategorySummary summarizes nearby historical disaster monuments.
type LoreCategorySummary struct {
	TotalCount   int            `json:"totalCount"`
	ByCategory   map[string]int `json:"byCategory"`
	EarliestYear int            `json:"earliestYear,omitempty"`
	LatestYear   int            `json:"latestYear,omitempty"`
}

// DisasterHistoryResult summarizes disaster history, statistics, and nearby lore.
type DisasterHistoryResult struct {
	TotalCount      int                   `json:"totalCount"`
	ActiveCount     int                   `json:"activeCount"`
	CriticalCount   int                   `json:"criticalCount"`
	WarningCount    int                   `json:"warningCount"`
	ByCategory      map[string]int        `json:"byCategory"`
	BySource        map[string]int        `json:"bySource"`
	Buckets         []DisasterBucketPoint `json:"buckets"`
	Events          []DisasterRecord      `json:"events"`
	NearbyLore      LoreCategorySummary   `json:"nearbyLore"`
	EarliestAt      *time.Time            `json:"earliestAt,omitempty"`
	LatestAt        *time.Time            `json:"latestAt,omitempty"`
}

// RiverHistoryQuery defines parameters for querying a river station's water level history.
type RiverHistoryQuery struct {
	StationID string    `json:"stationId"`
	FromTime  time.Time `json:"fromTime"`
	ToTime    time.Time `json:"toTime"`
}

// RiverHistoryResult returns time-series points and summary statistics for a river station.
type RiverHistoryResult struct {
	StationID     string                 `json:"stationId"`
	StationName   string                 `json:"stationName"`
	RiverName     string                 `json:"riverName"`
	DangerLevelM  float64                `json:"dangerLevelM"`
	WarningLevelM float64                `json:"warningLevelM"`
	MaxLevelM     float64                `json:"maxLevelM"`
	MinLevelM     float64                `json:"minLevelM"`
	LatestLevelM  float64                `json:"latestLevelM"`
	Points        []RiverTimeSeriesPoint `json:"points"`
}

// DisasterLoreQuery defines spatial/attribute filters for querying natural disaster monuments.
type DisasterLoreQuery struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	RadiusKm  float64 `json:"radiusKm"`
	MinLat    float64 `json:"minLat"`
	MaxLat    float64 `json:"maxLat"`
	MinLon    float64 `json:"minLon"`
	MaxLon    float64 `json:"maxLon"`
	Category  string  `json:"category,omitempty"`
	Query     string  `json:"query,omitempty"`
	Limit     int     `json:"limit,omitempty"`
}

func disasterPrimaryKey(source, id string) []byte {
	return []byte(source + ":" + id)
}

func disasterTimeIdxKey(t time.Time, source, id string) []byte {
	return []byte(fmt.Sprintf("%s:%s:%s", t.UTC().Format("20060102150405"), source, id))
}

// SaveDisasterBatch persists or updates disaster records and maintains their time-ordered index.
func (s *Store) SaveDisasterBatch(items []DisasterRecord) error {
	if s == nil || s.db == nil || len(items) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasters)
		idx := tx.Bucket(bucketDisasterTimeIdx)
		for _, item := range items {
			if item.ID == "" || item.Source == "" {
				continue
			}
			if item.StartedAt.IsZero() {
				item.StartedAt = time.Now().UTC()
			}
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = item.StartedAt
			}
			pk := disasterPrimaryKey(item.Source, item.ID)

			// Preserve original StartedAt if updating an existing record so index key stays stable.
			if existingRaw := b.Get(pk); existingRaw != nil {
				var existing DisasterRecord
				if err := json.Unmarshal(existingRaw, &existing); err == nil && !existing.StartedAt.IsZero() {
					if !existing.StartedAt.Equal(item.StartedAt) {
						_ = idx.Delete(disasterTimeIdxKey(existing.StartedAt, item.Source, item.ID))
					}
				}
			}

			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if err := b.Put(pk, raw); err != nil {
				return err
			}
			if err := idx.Put(disasterTimeIdxKey(item.StartedAt, item.Source, item.ID), pk); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListRecentDisasters returns up to limit disaster records ordered newest-first.
func (s *Store) ListRecentDisasters(limit int, activeOnly bool) ([]DisasterRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var out []DisasterRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasters)
		idx := tx.Bucket(bucketDisasterTimeIdx)
		c := idx.Cursor()
		for k, pk := c.Last(); k != nil && len(out) < limit; k, pk = c.Prev() {
			raw := b.Get(pk)
			if raw == nil {
				continue
			}
			var rec DisasterRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				continue
			}
			if activeOnly && !rec.IsActive {
				continue
			}
			out = append(out, rec)
		}
		return nil
	})
	return out, err
}

// QueryDisasterHistory analyzes persisted disaster events and nearby historical monuments.
func (s *Store) QueryDisasterHistory(req DisasterHistoryQuery) (DisasterHistoryResult, error) {
	res := DisasterHistoryResult{
		ByCategory: make(map[string]int),
		BySource:   make(map[string]int),
		NearbyLore: LoreCategorySummary{ByCategory: make(map[string]int)},
	}
	if s == nil || s.db == nil {
		return res, nil
	}

	catSet := make(map[string]struct{}, len(req.Categories))
	for _, c := range req.Categories {
		if c != "" {
			catSet[c] = struct{}{}
		}
	}
	srcSet := make(map[string]struct{}, len(req.Sources))
	for _, src := range req.Sources {
		if src != "" {
			srcSet[src] = struct{}{}
		}
	}

	var matched []DisasterRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasters)
		idx := tx.Bucket(bucketDisasterTimeIdx)
		c := idx.Cursor()

		var k, pk []byte
		if !req.ToTime.IsZero() {
			seekKey := []byte(req.ToTime.UTC().Format("20060102150405") + "\xff")
			k, pk = c.Seek(seekKey)
			if k == nil {
				k, pk = c.Last()
			} else {
				// Step back if Seek landed past ToTime
				for k != nil && string(k) > string(seekKey) {
					k, pk = c.Prev()
				}
			}
		} else {
			k, pk = c.Last()
		}

		for ; k != nil; k, pk = c.Prev() {
			raw := b.Get(pk)
			if raw == nil {
				continue
			}
			var rec DisasterRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				continue
			}
			if !req.FromTime.IsZero() && rec.StartedAt.Before(req.FromTime) {
				break
			}
			if !req.ToTime.IsZero() && rec.StartedAt.After(req.ToTime) {
				continue
			}
			if len(catSet) > 0 {
				if _, ok := catSet[rec.Category]; !ok {
					continue
				}
			}
			if len(srcSet) > 0 {
				if _, ok := srcSet[rec.Source]; !ok {
					continue
				}
			}
			if req.RadiusKm > 0 {
				if rec.Latitude == 0 && rec.Longitude == 0 {
					continue
				}
				if haversineKm(req.Latitude, req.Longitude, rec.Latitude, rec.Longitude) > req.RadiusKm {
					continue
				}
			}

			matched = append(matched, rec)
			res.TotalCount++
			if rec.IsActive {
				res.ActiveCount++
			}
			switch rec.Severity {
			case "critical":
				res.CriticalCount++
			case "warning":
				res.WarningCount++
			}
			res.ByCategory[rec.Category]++
			res.BySource[rec.Source]++
			if res.EarliestAt == nil || rec.StartedAt.Before(*res.EarliestAt) {
				t := rec.StartedAt
				res.EarliestAt = &t
			}
			if res.LatestAt == nil || rec.StartedAt.After(*res.LatestAt) {
				t := rec.StartedAt
				res.LatestAt = &t
			}
		}

		// Summarize nearby natural disaster lore if a spatial center is given.
		if req.RadiusKm > 0 {
			loreBucket := tx.Bucket(bucketDisasterLore)
			lc := loreBucket.Cursor()
			for lk, lv := lc.First(); lk != nil; lk, lv = lc.Next() {
				var lore DisasterLoreRecord
				if err := json.Unmarshal(lv, &lore); err != nil {
					continue
				}
				if haversineKm(req.Latitude, req.Longitude, lore.Latitude, lore.Longitude) <= req.RadiusKm {
					res.NearbyLore.TotalCount++
					res.NearbyLore.ByCategory[lore.Category]++
					if lore.Year > 0 {
						if res.NearbyLore.EarliestYear == 0 || lore.Year < res.NearbyLore.EarliestYear {
							res.NearbyLore.EarliestYear = lore.Year
						}
						if lore.Year > res.NearbyLore.LatestYear {
							res.NearbyLore.LatestYear = lore.Year
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return res, err
	}

	granularity := req.Granularity
	if granularity == "" || granularity == "auto" {
		span := time.Duration(0)
		if res.EarliestAt != nil && res.LatestAt != nil {
			span = res.LatestAt.Sub(*res.EarliestAt)
		}
		switch {
		case span > 365*24*time.Hour:
			granularity = "yearly"
		case span > 60*24*time.Hour:
			granularity = "monthly"
		default:
			granularity = "daily"
		}
	}

	bucketMap := make(map[string]*DisasterBucketPoint)
	for _, rec := range matched {
		var label string
		switch granularity {
		case "yearly":
			label = rec.StartedAt.UTC().Format("2006")
		case "monthly":
			label = rec.StartedAt.UTC().Format("2006-01")
		default:
			label = rec.StartedAt.UTC().Format("2006-01-02")
		}
		bp, ok := bucketMap[label]
		if !ok {
			bp = &DisasterBucketPoint{Label: label}
			bucketMap[label] = bp
		}
		bp.Count++
		if rec.Severity == "critical" {
			bp.CriticalCount++
		} else if rec.Severity == "warning" {
			bp.WarningCount++
		}
	}

	buckets := make([]DisasterBucketPoint, 0, len(bucketMap))
	for _, bp := range bucketMap {
		buckets = append(buckets, *bp)
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Label < buckets[j].Label
	})
	res.Buckets = buckets

	if len(matched) > 200 {
		matched = matched[:200]
	}
	res.Events = matched
	return res, nil
}

// SaveRiverTimeSeriesBatch persists river water level observations.
func (s *Store) SaveRiverTimeSeriesBatch(pts []RiverTimeSeriesPoint) error {
	if s == nil || s.db == nil || len(pts) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRiverTimeSeries)
		for _, pt := range pts {
			if pt.StationID == "" || pt.Timestamp == "" {
				continue
			}
			key := []byte(pt.StationID + ":" + pt.Timestamp)
			raw, err := json.Marshal(pt)
			if err != nil {
				return err
			}
			if err := b.Put(key, raw); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListLatestRiverStations returns the most recent observation for each stored river station.
func (s *Store) ListLatestRiverStations() ([]RiverTimeSeriesPoint, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	latestByStation := make(map[string]RiverTimeSeriesPoint)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRiverTimeSeries)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var pt RiverTimeSeriesPoint
			if err := json.Unmarshal(v, &pt); err != nil {
				continue
			}
			// Keys are sorted lexicographically: stationID:YYYYMMDDHHmm, so later keys overwrite earlier ones.
			latestByStation[pt.StationID] = pt
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]RiverTimeSeriesPoint, 0, len(latestByStation))
	for _, pt := range latestByStation {
		out = append(out, pt)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StationName < out[j].StationName
	})
	return out, nil
}

// QueryRiverHistory returns time-series water level records and summary stats for a station.
func (s *Store) QueryRiverHistory(req RiverHistoryQuery) (RiverHistoryResult, error) {
	res := RiverHistoryResult{StationID: req.StationID}
	if s == nil || s.db == nil || req.StationID == "" {
		return res, nil
	}
	prefix := []byte(req.StationID + ":")
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRiverTimeSeries)
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var pt RiverTimeSeriesPoint
			if err := json.Unmarshal(v, &pt); err != nil {
				continue
			}
			if !req.FromTime.IsZero() && pt.ObservedAt.Before(req.FromTime) {
				continue
			}
			if !req.ToTime.IsZero() && pt.ObservedAt.After(req.ToTime) {
				continue
			}
			if len(res.Points) == 0 {
				res.MinLevelM = pt.WaterLevelM
				res.MaxLevelM = pt.WaterLevelM
			} else {
				if pt.WaterLevelM < res.MinLevelM {
					res.MinLevelM = pt.WaterLevelM
				}
				if pt.WaterLevelM > res.MaxLevelM {
					res.MaxLevelM = pt.WaterLevelM
				}
			}
			res.StationName = pt.StationName
			res.RiverName = pt.RiverName
			if pt.DangerLevelM > 0 {
				res.DangerLevelM = pt.DangerLevelM
			}
			if pt.WarningLevelM > 0 {
				res.WarningLevelM = pt.WarningLevelM
			}
			res.LatestLevelM = pt.WaterLevelM
			res.Points = append(res.Points, pt)
		}
		return nil
	})
	return res, err
}

// SaveDisasterLoreBatch persists GSI natural disaster monument records locally.
func (s *Store) SaveDisasterLoreBatch(items []DisasterLoreRecord) error {
	if s == nil || s.db == nil || len(items) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasterLore)
		for _, item := range items {
			if item.ID == "" {
				continue
			}
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(item.ID), raw); err != nil {
				return err
			}
		}
		return nil
	})
}

// CountDisasterLore returns the number of locally cached natural disaster monuments.
func (s *Store) CountDisasterLore() (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var count int
	err := s.db.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(bucketDisasterLore).Stats().KeyN
		return nil
	})
	return count, err
}

// QueryDisasterLore filters cached natural disaster monuments by bounding box, radius, category, or text.
func (s *Store) QueryDisasterLore(req DisasterLoreQuery) ([]DisasterLoreRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 500
	}
	hasBBox := req.MaxLat > req.MinLat && req.MaxLon > req.MinLon
	qLower := strings.ToLower(strings.TrimSpace(req.Query))

	var out []DisasterLoreRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasterLore)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var rec DisasterLoreRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				continue
			}
			if req.Category != "" && req.Category != "all" && rec.Category != req.Category {
				continue
			}
			if hasBBox {
				if rec.Latitude < req.MinLat || rec.Latitude > req.MaxLat ||
					rec.Longitude < req.MinLon || rec.Longitude > req.MaxLon {
					continue
				}
			}
			if req.RadiusKm > 0 {
				if haversineKm(req.Latitude, req.Longitude, rec.Latitude, rec.Longitude) > req.RadiusKm {
					continue
				}
			}
			if qLower != "" {
				haystack := strings.ToLower(rec.Name + " " + rec.Prefecture + " " + rec.Municipality + " " + rec.DisasterKind + " " + rec.Summary)
				if !strings.Contains(haystack, qLower) {
					continue
				}
			}
			out = append(out, rec)
			if len(out) >= limit {
				break
			}
		}
		return nil
	})
	return out, err
}

// GetFeedCursor retrieves a stored feed cursor by key.
func (s *Store) GetFeedCursor(key string) (FeedCursor, bool, error) {
	if s == nil || s.db == nil {
		return FeedCursor{}, false, nil
	}
	var cur FeedCursor
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketFeedCursors).Get([]byte(key))
		if v == nil {
			return nil
		}
		if err := json.Unmarshal(v, &cur); err != nil {
			return err
		}
		found = true
		return nil
	})
	return cur, found, err
}

// SaveFeedCursor persists a feed cursor.
func (s *Store) SaveFeedCursor(cur FeedCursor) error {
	if s == nil || s.db == nil || cur.Key == "" {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(cur)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketFeedCursors).Put([]byte(cur.Key), raw)
	})
}
