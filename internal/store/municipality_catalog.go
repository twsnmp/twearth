package store

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

//go:embed data/municipalities.json.gz
var embeddedMunicipalitiesGz []byte

var (
	builtinMuniOnce sync.Once
	builtinMuniList []MunicipalityEntry
	builtinMuniMap  map[string]MunicipalityEntry
	rankingStateKey = []byte("current")
)

// MunicipalityEntry represents a target municipality or city for livability/safety ranking.
type MunicipalityEntry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	NameEn       string    `json:"nameEn"`
	Prefecture   string    `json:"prefecture,omitempty"`
	PrefectureEn string    `json:"prefectureEn,omitempty"`
	Country      string    `json:"country,omitempty"`
	CountryEn    string    `json:"countryEn,omitempty"`
	Region       string    `json:"region,omitempty"` // "hokkaido" | "tohoku" | "kanto" | "chubu" | "kinki" | "chugoku" | "shikoku" | "kyushu" | "asia" | "north_america" | "europe" | "oceania" | "latin_america" | "middle_east" | "africa" | "custom"
	Group        string    `json:"group"`            // "japan" | "global" | "custom"
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	Source       string    `json:"source"` // "builtin" | "custom" | "observed"
	CreatedAt    time.Time `json:"createdAt,omitempty"`
}

// MunicipalityClimateStats stores pre-aggregated or Open-Meteo fetched daily climate statistics for a municipality.
type MunicipalityClimateStats struct {
	MunicipalityID string    `json:"municipalityId"`
	RangeKey       string    `json:"rangeKey"`
	SampleDays     int       `json:"sampleDays"`
	MeanTempC      float64   `json:"meanTempC"`
	MaxTempC       float64   `json:"maxTempC"`
	MinTempC       float64   `json:"minTempC"`
	HotDays30      int       `json:"hotDays30"`     // Days with max temp >= 30°C (annualized)
	ExtremeHot35   int       `json:"extremeHot35"`  // Days with max temp >= 35°C (annualized)
	FreezingDays0  int       `json:"freezingDays0"` // Days with min temp < 0°C (annualized)
	AnnualPrecipMm float64   `json:"annualPrecipMm"`
	HeavyRainDays  int       `json:"heavyRainDays"` // Days with precip >= 50mm (annualized)
	AnnualSnowCm   float64   `json:"annualSnowCm"`
	MaxWindKmh     float64   `json:"maxWindKmh"`
	Source         string    `json:"source"` // "direct" | "interpolated"
	UpdatedAt      time.Time `json:"updatedAt"`
}

// RankingCacheEntry stores a persisted municipality ranking result row in bbolt.
type RankingCacheEntry struct {
	ID          string          `json:"id"`
	TotalScore  float64         `json:"totalScore"`
	PayloadJSON json.RawMessage `json:"payloadJson"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// RankingSavedState persists the current ranking job configuration and progress cursor.
type RankingSavedState struct {
	Status     string          `json:"status"` // "idle" | "running" | "paused" | "completed" | "error"
	ConfigJSON json.RawMessage `json:"configJson"`
	Total      int             `json:"total"`
	Cursor     int             `json:"cursor"`
	CacheHits  int             `json:"cacheHits"`
	Computed   int             `json:"computed"`
	Skipped    int             `json:"skipped"`
	Errors     int             `json:"errors"`
	LastError  string          `json:"lastError,omitempty"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

func loadBuiltinMunicipalities() {
	builtinMuniOnce.Do(func() {
		builtinMuniMap = make(map[string]MunicipalityEntry, 2100)
		if len(embeddedMunicipalitiesGz) == 0 {
			return
		}
		gr, err := gzip.NewReader(bytes.NewReader(embeddedMunicipalitiesGz))
		if err != nil {
			return
		}
		defer func() { _ = gr.Close() }()
		raw, err := io.ReadAll(gr)
		if err != nil {
			return
		}
		var list []MunicipalityEntry
		if err := json.Unmarshal(raw, &list); err != nil {
			return
		}
		builtinMuniList = list
		for _, m := range list {
			builtinMuniMap[m.ID] = m
		}
	})
}

// BuiltinMunicipalities returns a copy of all built-in Japanese municipalities and major global cities.
func BuiltinMunicipalities() []MunicipalityEntry {
	loadBuiltinMunicipalities()
	out := make([]MunicipalityEntry, len(builtinMuniList))
	copy(out, builtinMuniList)
	return out
}

// GetBuiltinMunicipality looks up a built-in municipality by ID.
func GetBuiltinMunicipality(id string) (MunicipalityEntry, bool) {
	loadBuiltinMunicipalities()
	m, ok := builtinMuniMap[id]
	return m, ok
}

// PreIsSeaOfJapan returns true if the municipality belongs to a prefecture facing the Sea of Japan with heavy winter snow.
func (m MunicipalityEntry) PreIsSeaOfJapan() bool {
	switch m.PrefectureEn {
	case "Aomori", "Akita", "Yamagata", "Niigata", "Toyama", "Ishikawa", "Fukui", "Tottori", "Shimane":
		return true
	default:
		return false
	}
}

// ListAllMunicipalities returns all built-in municipalities merged with user-registered custom municipalities
// and any user-configured observation cities not already present.
func (s *Store) ListAllMunicipalities() ([]MunicipalityEntry, error) {
	base := BuiltinMunicipalities()
	seen := make(map[string]bool, len(base)+32)
	for _, m := range base {
		seen[m.ID] = true
	}

	custom, err := s.ListCustomMunicipalities()
	if err != nil {
		return base, err
	}
	for _, c := range custom {
		if !seen[c.ID] {
			seen[c.ID] = true
			base = append(base, c)
		}
	}

	if s != nil && s.db != nil {
		if cfg, err := s.GetConfig(); err == nil {
			for _, loc := range cfg.Locations {
				id := "obs-" + loc.ID
				if seen[id] || seen[loc.ID] {
					continue
				}
				seen[id] = true
				grp := "global"
				country := "World"
				countryJa := "世界"
				region := "custom"
				if isJapanCoord(loc.Latitude, loc.Longitude) {
					grp = "japan"
					country = "Japan"
					countryJa = "日本"
				}
				base = append(base, MunicipalityEntry{
					ID:           id,
					Name:         loc.Name,
					NameEn:       loc.NameEn,
					Prefecture:   countryJa,
					PrefectureEn: country,
					Country:      countryJa,
					CountryEn:    country,
					Region:       region,
					Group:        grp,
					Latitude:     loc.Latitude,
					Longitude:    loc.Longitude,
					Source:       "observed",
				})
			}
		}
	}

	return base, nil
}

// ListCustomMunicipalities returns user-registered custom municipalities stored in bbolt.
func (s *Store) ListCustomMunicipalities() ([]MunicipalityEntry, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []MunicipalityEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingMunicipalities)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var m MunicipalityEntry
			if err := json.Unmarshal(v, &m); err == nil && m.ID != "" {
				out = append(out, m)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, err
}

// SaveCustomMunicipality adds or updates a user-registered custom municipality in bbolt.
func (s *Store) SaveCustomMunicipality(m MunicipalityEntry) (MunicipalityEntry, error) {
	if s == nil || s.db == nil {
		return m, fmt.Errorf("store is not initialized")
	}
	m.Name = strings.TrimSpace(m.Name)
	m.NameEn = strings.TrimSpace(m.NameEn)
	if m.Name == "" && m.NameEn == "" {
		return m, fmt.Errorf("municipality name is required")
	}
	if m.Name == "" {
		m.Name = m.NameEn
	}
	if m.NameEn == "" {
		m.NameEn = m.Name
	}
	if m.ID == "" {
		m.ID = fmt.Sprintf("custom-%d", time.Now().UnixNano())
	}
	if m.Group == "" {
		if isJapanCoord(m.Latitude, m.Longitude) {
			m.Group = "japan"
		} else {
			m.Group = "global"
		}
	}
	if m.Country == "" {
		if m.Group == "japan" {
			m.Country = "日本"
			m.CountryEn = "Japan"
		} else {
			m.Country = "世界"
			m.CountryEn = "World"
		}
	}
	if m.CountryEn == "" {
		m.CountryEn = m.Country
	}
	if m.Prefecture == "" {
		m.Prefecture = m.Country
	}
	if m.PrefectureEn == "" {
		m.PrefectureEn = m.Prefecture
	}
	if m.Region == "" {
		m.Region = "custom"
	}
	m.Source = "custom"
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingMunicipalities)
		return b.Put([]byte(m.ID), raw)
	})
	return m, err
}

// DeleteCustomMunicipality deletes a user-registered custom municipality by ID.
func (s *Store) DeleteCustomMunicipality(id string) error {
	if s == nil || s.db == nil || id == "" {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingMunicipalities)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(id))
	})
}

func climateCacheKey(muniID, rangeKey string) []byte {
	return []byte(muniID + ":" + rangeKey)
}

// SaveMunicipalityClimateStats stores pre-aggregated climate stats for a municipality and range.
func (s *Store) SaveMunicipalityClimateStats(st MunicipalityClimateStats) error {
	if s == nil || s.db == nil || st.MunicipalityID == "" {
		return nil
	}
	if st.RangeKey == "" {
		st.RangeKey = "1y"
	}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingClimateCache)
		return b.Put(climateCacheKey(st.MunicipalityID, st.RangeKey), raw)
	})
}

// GetMunicipalityClimateStats retrieves cached climate stats for a municipality and range.
func (s *Store) GetMunicipalityClimateStats(muniID, rangeKey string) (MunicipalityClimateStats, bool, error) {
	var out MunicipalityClimateStats
	if s == nil || s.db == nil || muniID == "" {
		return out, false, nil
	}
	if rangeKey == "" {
		rangeKey = "1y"
	}
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingClimateCache)
		if b == nil {
			return nil
		}
		raw := b.Get(climateCacheKey(muniID, rangeKey))
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &out)
	})
	return out, found, err
}

// ListMunicipalityClimateStats returns all cached climate stats for the given rangeKey (or all if empty).
func (s *Store) ListMunicipalityClimateStats(rangeKey string) (map[string]MunicipalityClimateStats, error) {
	out := make(map[string]MunicipalityClimateStats)
	if s == nil || s.db == nil {
		return out, nil
	}
	suffix := ""
	if rangeKey != "" {
		suffix = ":" + rangeKey
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingClimateCache)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			if suffix != "" && !strings.HasSuffix(string(k), suffix) {
				return nil
			}
			var st MunicipalityClimateStats
			if err := json.Unmarshal(v, &st); err == nil && st.MunicipalityID != "" {
				out[st.MunicipalityID] = st
			}
			return nil
		})
	})
	return out, err
}

// SaveRankingCacheBatch persists multiple municipality ranking results in a single transaction.
func (s *Store) SaveRankingCacheBatch(entries []RankingCacheEntry) error {
	if s == nil || s.db == nil || len(entries) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingResults)
		for _, e := range entries {
			if e.ID == "" {
				continue
			}
			if e.UpdatedAt.IsZero() {
				e.UpdatedAt = time.Now().UTC()
			}
			raw, err := json.Marshal(e)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(e.ID), raw); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListRankingCacheEntries returns all persisted municipality ranking results sorted by TotalScore descending.
func (s *Store) ListRankingCacheEntries() ([]RankingCacheEntry, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []RankingCacheEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingResults)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var e RankingCacheEntry
			if err := json.Unmarshal(v, &e); err == nil && e.ID != "" {
				out = append(out, e)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalScore != out[j].TotalScore {
			return out[i].TotalScore > out[j].TotalScore
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ClearRankingCache deletes all cached ranking results (and optionally climate cache if clearClimate is true).
func (s *Store) ClearRankingCache(clearClimate bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := recreateBucketTx(tx, bucketRankingResults); err != nil {
			return err
		}
		if clearClimate {
			if err := recreateBucketTx(tx, bucketRankingClimateCache); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveRankingState persists the current ranking job progress and configuration.
func (s *Store) SaveRankingState(st RankingSavedState) error {
	if s == nil || s.db == nil {
		return nil
	}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingState)
		return b.Put(rankingStateKey, raw)
	})
}

// LoadRankingState loads the persisted ranking job progress and configuration.
func (s *Store) LoadRankingState() (RankingSavedState, bool, error) {
	var out RankingSavedState
	if s == nil || s.db == nil {
		return out, false, nil
	}
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRankingState)
		if b == nil {
			return nil
		}
		raw := b.Get(rankingStateKey)
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &out)
	})
	return out, found, err
}

// ListAllQuakesSince returns all stored QuakeRecords occurring at or after since.
func (s *Store) ListAllQuakesSince(since time.Time) ([]QuakeRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []QuakeRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketQuakes)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		var k, v []byte
		if !since.IsZero() {
			seekKey := []byte(since.UTC().Format("20060102T150405"))
			k, v = c.Seek(seekKey)
		} else {
			k, v = c.First()
		}
		for ; k != nil; k, v = c.Next() {
			var q QuakeRecord
			if err := json.Unmarshal(v, &q); err != nil {
				continue
			}
			if !since.IsZero() && q.OccurredAt.Before(since) {
				continue
			}
			out = append(out, q)
		}
		return nil
	})
	return out, err
}

// ListAllDisastersSince returns all stored DisasterRecords occurring at or after since.
func (s *Store) ListAllDisastersSince(since time.Time) ([]DisasterRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []DisasterRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasters)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var d DisasterRecord
			if err := json.Unmarshal(v, &d); err != nil {
				return nil
			}
			if !since.IsZero() && d.StartedAt.Before(since) && d.UpdatedAt.Before(since) {
				return nil
			}
			out = append(out, d)
			return nil
		})
	})
	return out, err
}

// ListAllDisasterLore returns all stored DisasterLoreRecords.
func (s *Store) ListAllDisasterLore() ([]DisasterLoreRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []DisasterLoreRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDisasterLore)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var l DisasterLoreRecord
			if err := json.Unmarshal(v, &l); err == nil {
				out = append(out, l)
			}
			return nil
		})
	})
	return out, err
}

// ComputeLocationClimateStats derives MunicipalityClimateStats from daily (or hourly) weather history stored for a configured locationID.
func (s *Store) ComputeLocationClimateStats(locationID, rangeKey string, since time.Time) (MunicipalityClimateStats, bool) {
	if s == nil || s.db == nil || locationID == "" {
		return MunicipalityClimateStats{}, false
	}
	fromDate := ""
	if !since.IsZero() {
		fromDate = since.UTC().Format("20060102")
	}
	prefix := []byte(locationID + ":")
	fromKey := prefix
	if fromDate != "" {
		fromKey = []byte(locationID + ":" + fromDate)
	}

	var pts []DailyWeatherPoint
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDailyTimeSeries)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.Seek(fromKey); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var dp DailyWeatherPoint
			if err := json.Unmarshal(v, &dp); err == nil && len(dp.Date) == 8 {
				pts = append(pts, dp)
			}
		}
		return nil
	})

	if len(pts) > 0 {
		return SummarizeDailyWeatherPoints(locationID, rangeKey, pts, "direct"), true
	}

	// Fallback to hourly bucket if daily is not yet populated
	fromTS := ""
	if !since.IsZero() {
		fromTS = since.UTC().Format("200601021504")
	}
	hSamples, _, _, err := s.loadHourlySamples("weather", locationID, fromTS, time.Now().UTC().Format("200601021504"), "temperature2m")
	if err != nil || len(hSamples) == 0 {
		return MunicipalityClimateStats{}, false
	}
	sum := 0.0
	minT := hSamples[0].val
	maxT := hSamples[0].val
	for _, sm := range hSamples {
		sum += sm.val
		if sm.val < minT {
			minT = sm.val
		}
		if sm.val > maxT {
			maxT = sm.val
		}
	}
	days := len(hSamples) / 24
	if days < 1 {
		days = 1
	}
	return MunicipalityClimateStats{
		MunicipalityID: locationID,
		RangeKey:       rangeKey,
		SampleDays:     days,
		MeanTempC:      round2(sum / float64(len(hSamples))),
		MaxTempC:       round2(maxT),
		MinTempC:       round2(minT),
		Source:         "direct",
		UpdatedAt:      time.Now().UTC(),
	}, true
}

// SummarizeDailyWeatherPoints aggregates a slice of DailyWeatherPoints into annualized MunicipalityClimateStats.
func SummarizeDailyWeatherPoints(id, rangeKey string, pts []DailyWeatherPoint, source string) MunicipalityClimateStats {
	n := len(pts)
	if n == 0 {
		return MunicipalityClimateStats{MunicipalityID: id, RangeKey: rangeKey, Source: source}
	}
	sumTemp := 0.0
	maxTemp := pts[0].TempMax
	minTemp := pts[0].TempMin
	var hot30, hot35, freeze0, heavyRain int
	var sumPrecip, sumSnow, maxWind float64

	for _, pt := range pts {
		sumTemp += pt.TempMean
		if pt.TempMax > maxTemp {
			maxTemp = pt.TempMax
		}
		if pt.TempMin < minTemp {
			minTemp = pt.TempMin
		}
		if pt.TempMax >= 30.0 {
			hot30++
		}
		if pt.TempMax >= 35.0 {
			hot35++
		}
		if pt.TempMin < 0.0 {
			freeze0++
		}
		sumPrecip += pt.PrecipitationSum
		if pt.PrecipitationSum >= 50.0 {
			heavyRain++
		}
		sumSnow += pt.SnowfallSum
		if pt.WindSpeedMax > maxWind {
			maxWind = pt.WindSpeedMax
		}
	}

	scale := 365.0 / float64(n)
	return MunicipalityClimateStats{
		MunicipalityID: id,
		RangeKey:       rangeKey,
		SampleDays:     n,
		MeanTempC:      round2(sumTemp / float64(n)),
		MaxTempC:       round2(maxTemp),
		MinTempC:       round2(minTemp),
		HotDays30:      int(float64(hot30)*scale + 0.5),
		ExtremeHot35:   int(float64(hot35)*scale + 0.5),
		FreezingDays0:  int(float64(freeze0)*scale + 0.5),
		AnnualPrecipMm: round2(sumPrecip * scale),
		HeavyRainDays:  int(float64(heavyRain)*scale + 0.5),
		AnnualSnowCm:   round2(sumSnow * scale),
		MaxWindKmh:     round2(maxWind),
		Source:         source,
		UpdatedAt:      time.Now().UTC(),
	}
}
