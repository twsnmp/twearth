package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// MarineTimeSeriesPoint represents a marine observation snapshot keyed by locationId:YYYYMMDDHHmm.
type MarineTimeSeriesPoint struct {
	LocationID            string  `json:"locationId"`
	Timestamp             string  `json:"timestamp"`
	SeaSurfaceTemperature float64 `json:"seaSurfaceTemperature"`
	WaveHeight            float64 `json:"waveHeight"`
	OceanCurrentVelocity  float64 `json:"oceanCurrentVelocity"`
	OceanCurrentDirection float64 `json:"oceanCurrentDirection"`
	MissingSST            bool    `json:"missingSst,omitempty"`
	MissingWave           bool    `json:"missingWave,omitempty"`
	MissingCurrent        bool    `json:"missingCurrent,omitempty"`
}

// HistorySyncState persists background backfill and catch-up progress per target.
type HistorySyncState struct {
	Kind            string    `json:"kind"` // "weather" | "marine" | "quake"
	LocationID      string    `json:"locationId"`
	EarliestTS      string    `json:"earliestTs"`
	LatestTS        string    `json:"latestTs"`
	TargetStartDate string    `json:"targetStartDate"` // YYYY-MM-DD
	BackfillDone    bool      `json:"backfillDone"`
	LastError       string    `json:"lastError,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// QuakeRecord represents a persisted earthquake/tsunami event.
type QuakeRecord struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Code       int       `json:"code,omitempty"`
	Title      string    `json:"title"`
	Hypocenter string    `json:"hypocenter"`
	Magnitude  float64   `json:"magnitude"`
	MaxScale   string    `json:"maxScale"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	DepthKm    float64   `json:"depthKm"`
	Tsunami    string    `json:"tsunami,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// HistoryQuery defines parameters for querying and aggregating time-series history.
type HistoryQuery struct {
	Kind            string `json:"kind"`            // "weather" | "marine"
	LocationID      string `json:"locationId"`      // required
	FromTS          string `json:"fromTs"`          // YYYYMMDDHHmm (optional)
	ToTS            string `json:"toTs"`            // YYYYMMDDHHmm (optional)
	Metric          string `json:"metric"`          // e.g. "temperature2m", "seaSurfaceTemperature"
	Granularity     string `json:"granularity"`     // "auto" | "hourly" | "daily" | "monthly"
	MovingAvgWindow int    `json:"movingAvgWindow"` // 0 -> default (7 for daily/hourly, 3 for monthly)
}

// HistoryDataPoint represents a single (raw or resampled) point in a history series.
type HistoryDataPoint struct {
	Timestamp string   `json:"timestamp"`
	Label     string   `json:"label"`
	Value     float64  `json:"value"`
	Min       float64  `json:"min"`
	Max       float64  `json:"max"`
	Count     int      `json:"count"`
	MovingAvg *float64 `json:"movingAvg,omitempty"`
}

// HistoryStats summarizes statistical properties of the queried series.
type HistoryStats struct {
	Count        int     `json:"count"`
	Mean         float64 `json:"mean"`
	Min          float64 `json:"min"`
	MinTimestamp string  `json:"minTimestamp"`
	Max          float64 `json:"max"`
	MaxTimestamp string  `json:"maxTimestamp"`
	Median       float64 `json:"median"`
	StdDev       float64 `json:"stdDev"`
	Delta        float64 `json:"delta"`
	TrendPerDay  float64 `json:"trendPerDay"`
}

// HistoryResult is returned by QueryHistory.
type HistoryResult struct {
	Kind        string             `json:"kind"`
	LocationID  string             `json:"locationId"`
	Metric      string             `json:"metric"`
	Granularity string             `json:"granularity"`
	Points      []HistoryDataPoint `json:"points"`
	Stats       HistoryStats       `json:"stats"`
	EarliestTS  string             `json:"earliestTs"`
	LatestTS    string             `json:"latestTs"`
}

// QuakeHistoryQuery defines parameters for analyzing historical earthquakes near a coordinate.
type QuakeHistoryQuery struct {
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	RadiusKm    float64   `json:"radiusKm"` // <=0 means global
	FromTime    time.Time `json:"fromTime"`
	ToTime      time.Time `json:"toTime"`
	Granularity string    `json:"granularity"` // "auto" | "daily" | "monthly"
}

// QuakeBucketPoint aggregates earthquake counts and magnitudes for a time bucket.
type QuakeBucketPoint struct {
	Label        string  `json:"label"`
	Count        int     `json:"count"`
	MaxMagnitude float64 `json:"maxMagnitude"`
	AvgMagnitude float64 `json:"avgMagnitude"`
}

// QuakeHistoryResult summarizes earthquake history and statistics.
type QuakeHistoryResult struct {
	TotalCount   int                `json:"totalCount"`
	MaxMagnitude float64            `json:"maxMagnitude"`
	AvgMagnitude float64            `json:"avgMagnitude"`
	MaxScale     string             `json:"maxScale"`
	Granularity  string             `json:"granularity"`
	Buckets      []QuakeBucketPoint `json:"buckets"`
	Recent       []QuakeRecord      `json:"recent"`
}

// SaveTimeSeriesBatch stores multiple weather TimeSeriesPoints in a single transaction.
func (s *Store) SaveTimeSeriesBatch(pts []TimeSeriesPoint) error {
	if len(pts) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketTimeSeries)
		for _, pt := range pts {
			if pt.Timestamp == "" {
				continue
			}
			key := pt.Timestamp
			if pt.LocationID != "" {
				key = pt.LocationID + ":" + pt.Timestamp
			}
			data, err := json.Marshal(pt)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(key), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveMarineTimeSeries stores a single marine snapshot.
func (s *Store) SaveMarineTimeSeries(pt MarineTimeSeriesPoint) error {
	return s.SaveMarineTimeSeriesBatch([]MarineTimeSeriesPoint{pt})
}

// SaveMarineTimeSeriesBatch stores multiple MarineTimeSeriesPoints in a single transaction.
func (s *Store) SaveMarineTimeSeriesBatch(pts []MarineTimeSeriesPoint) error {
	if len(pts) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMarineTimeSeries)
		for _, pt := range pts {
			if pt.LocationID == "" || pt.Timestamp == "" {
				continue
			}
			key := pt.LocationID + ":" + pt.Timestamp
			data, err := json.Marshal(pt)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(key), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetTimeSeriesBounds returns the earliest and latest YYYYMMDDHHmm timestamps and point count for a location.
func (s *Store) GetTimeSeriesBounds(kind, locationID string) (earliestTS, latestTS string, count int, err error) {
	if locationID == "" {
		return "", "", 0, nil
	}
	bucketName := bucketTimeSeries
	if kind == "marine" {
		bucketName = bucketMarineTimeSeries
	}
	prefix := []byte(locationID + ":")
	err = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketName).Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			ts := strings.TrimPrefix(string(k), string(prefix))
			if earliestTS == "" {
				earliestTS = ts
			}
			latestTS = ts
			count++
		}
		return nil
	})
	return earliestTS, latestTS, count, err
}

func historySyncKey(kind, locationID string) []byte {
	return []byte(kind + ":" + locationID)
}

// GetHistorySyncState loads the sync state for a given kind and locationID.
func (s *Store) GetHistorySyncState(kind, locationID string) (HistorySyncState, bool, error) {
	var st HistorySyncState
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketHistorySync).Get(historySyncKey(kind, locationID))
		if raw == nil {
			return nil
		}
		if err := json.Unmarshal(raw, &st); err != nil {
			return err
		}
		found = true
		return nil
	})
	return st, found, err
}

// SaveHistorySyncState persists the sync state for a given kind and locationID.
func (s *Store) SaveHistorySyncState(st HistorySyncState) error {
	st.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketHistorySync).Put(historySyncKey(st.Kind, st.LocationID), data)
	})
}

// ListHistorySyncStates returns all persisted HistorySyncState entries.
func (s *Store) ListHistorySyncStates() ([]HistorySyncState, error) {
	var out []HistorySyncState
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketHistorySync).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if bytes.HasPrefix(k, []byte("meta:")) {
				continue
			}
			var st HistorySyncState
			if err := json.Unmarshal(v, &st); err != nil {
				continue
			}
			out = append(out, st)
		}
		return nil
	})
	return out, err
}

// GetMetaValue retrieves an arbitrary metadata string stored under meta:<key>.
func (s *Store) GetMetaValue(key string) (string, bool, error) {
	var val string
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketHistorySync).Get([]byte("meta:" + key))
		if raw != nil {
			val = string(raw)
			ok = true
		}
		return nil
	})
	return val, ok, err
}

// SetMetaValue stores an arbitrary metadata string under meta:<key>.
func (s *Store) SetMetaValue(key, val string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketHistorySync).Put([]byte("meta:"+key), []byte(val))
	})
}

type rawMetricSample struct {
	ts  string
	val float64
}

func extractWeatherMetric(pt TimeSeriesPoint, metric string) float64 {
	switch metric {
	case "pressureMsl":
		return pt.PressureMSL
	case "precipitation":
		return pt.Precipitation
	case "rain":
		return pt.Rain
	case "snowfall":
		return pt.Snowfall
	case "snowDepth":
		return pt.SnowDepth
	case "cape":
		return pt.CAPE
	case "kpIndex":
		return pt.KpIndex
	case "lightningCount":
		return float64(pt.LightningCount)
	case "seaSurfaceTemp":
		return pt.SeaSurfaceTemp
	default:
		return pt.Temperature2m
	}
}

func extractMarineMetric(pt MarineTimeSeriesPoint, metric string) float64 {
	switch metric {
	case "waveHeight":
		if pt.MissingWave {
			return math.NaN()
		}
		return pt.WaveHeight
	case "oceanCurrentVelocity":
		if pt.MissingCurrent {
			return math.NaN()
		}
		return pt.OceanCurrentVelocity
	case "oceanCurrentDirection":
		if pt.MissingCurrent {
			return math.NaN()
		}
		return pt.OceanCurrentDirection
	default:
		if pt.MissingSST {
			return math.NaN()
		}
		return pt.SeaSurfaceTemperature
	}
}

// QueryHistory retrieves, resamples, and computes statistics for a location's time series.
func (s *Store) QueryHistory(req HistoryQuery) (HistoryResult, error) {
	if req.Kind != "marine" {
		req.Kind = "weather"
	}
	if req.Metric == "" {
		if req.Kind == "marine" {
			req.Metric = "seaSurfaceTemperature"
		} else {
			req.Metric = "temperature2m"
		}
	}
	if req.ToTS == "" {
		req.ToTS = time.Now().UTC().Format("200601021504")
	}

	res := HistoryResult{
		Kind:       req.Kind,
		LocationID: req.LocationID,
		Metric:     req.Metric,
	}
	if req.LocationID == "" {
		res.Granularity = "hourly"
		return res, nil
	}

	var samples []rawMetricSample
	fromDaily := false
	useDaily := req.Granularity != "hourly" &&
		(isLongRangeQuery(req.FromTS, req.ToTS) ||
			req.Granularity == "yearly" ||
			req.Granularity == "monthly" ||
			req.Granularity == "daily")

	if useDaily {
		dSamples, dEarliest, dLatest, err := s.loadDailySamples(req.Kind, req.LocationID, req.FromTS, req.ToTS, req.Metric)
		if err != nil {
			return res, err
		}
		if len(dSamples) > 0 {
			samples = dSamples
			res.EarliestTS = dEarliest
			res.LatestTS = dLatest
			fromDaily = true
		}
	}

	if len(samples) == 0 {
		hSamples, hEarliest, hLatest, err := s.loadHourlySamples(req.Kind, req.LocationID, req.FromTS, req.ToTS, req.Metric)
		if err != nil {
			return res, err
		}
		samples = hSamples
		if res.EarliestTS == "" {
			res.EarliestTS = hEarliest
		}
		if res.LatestTS == "" {
			res.LatestTS = hLatest
		}
	}

	granularity := req.Granularity
	if granularity == "" || granularity == "auto" {
		granularity = chooseAutoGranularity(samples, fromDaily)
	}
	res.Granularity = granularity

	if len(samples) == 0 {
		return res, nil
	}

	res.Stats = computeStats(samples)
	points := aggregateSamples(samples, granularity)

	win := req.MovingAvgWindow
	if win <= 0 {
		switch granularity {
		case "yearly":
			win = 3
		case "monthly":
			win = 3
		case "daily":
			win = 7
		default:
			win = 12
		}
	}
	applyMovingAverage(points, win)
	res.Points = points
	return res, nil
}

func (s *Store) loadHourlySamples(kind, locationID, fromTS, toTS, metric string) (samples []rawMetricSample, earliestTS, latestTS string, err error) {
	bucketName := bucketTimeSeries
	if kind == "marine" {
		bucketName = bucketMarineTimeSeries
	}
	prefix := []byte(locationID + ":")
	fromKey := prefix
	if fromTS != "" {
		fromKey = []byte(locationID + ":" + fromTS)
	}
	toKey := []byte(locationID + ":" + toTS)

	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		c := b.Cursor()

		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			earliestTS = strings.TrimPrefix(string(k), string(prefix))
			break
		}
		endPrefix := []byte(locationID + ";")
		if k, _ := c.Seek(endPrefix); k != nil {
			if pk, _ := c.Prev(); pk != nil && bytes.HasPrefix(pk, prefix) {
				latestTS = strings.TrimPrefix(string(pk), string(prefix))
			}
		} else if lk, _ := c.Last(); lk != nil && bytes.HasPrefix(lk, prefix) {
			latestTS = strings.TrimPrefix(string(lk), string(prefix))
		}

		for k, v := c.Seek(fromKey); k != nil && bytes.HasPrefix(k, prefix) && bytes.Compare(k, toKey) <= 0; k, v = c.Next() {
			ts := strings.TrimPrefix(string(k), string(prefix))
			if len(ts) < 12 {
				continue
			}
			var val float64
			if kind == "marine" {
				var pt MarineTimeSeriesPoint
				if err := json.Unmarshal(v, &pt); err != nil {
					continue
				}
				val = extractMarineMetric(pt, metric)
			} else {
				var pt TimeSeriesPoint
				if err := json.Unmarshal(v, &pt); err != nil {
					continue
				}
				val = extractWeatherMetric(pt, metric)
			}
			if math.IsNaN(val) || math.IsInf(val, 0) {
				continue
			}
			samples = append(samples, rawMetricSample{ts: ts, val: val})
		}
		return nil
	})
	return samples, earliestTS, latestTS, err
}

func (s *Store) loadDailySamples(kind, locationID, fromTS, toTS, metric string) (samples []rawMetricSample, earliestTS, latestTS string, err error) {
	bucketName := bucketDailyTimeSeries
	if kind == "marine" {
		bucketName = bucketDailyMarine
	}
	prefix := []byte(locationID + ":")
	fromKey := prefix
	if len(fromTS) >= 8 {
		fromKey = []byte(locationID + ":" + fromTS[:8])
	}
	toDate := toTS
	if len(toDate) >= 8 {
		toDate = toDate[:8]
	}
	toKey := []byte(locationID + ":" + toDate)

	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}
		c := b.Cursor()

		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			earliestTS = strings.TrimPrefix(string(k), string(prefix)) + "0000"
			break
		}
		endPrefix := []byte(locationID + ";")
		if k, _ := c.Seek(endPrefix); k != nil {
			if pk, _ := c.Prev(); pk != nil && bytes.HasPrefix(pk, prefix) {
				latestTS = strings.TrimPrefix(string(pk), string(prefix)) + "0000"
			}
		} else if lk, _ := c.Last(); lk != nil && bytes.HasPrefix(lk, prefix) {
			latestTS = strings.TrimPrefix(string(lk), string(prefix)) + "0000"
		}

		for k, v := c.Seek(fromKey); k != nil && bytes.HasPrefix(k, prefix) && bytes.Compare(k, toKey) <= 0; k, v = c.Next() {
			d := strings.TrimPrefix(string(k), string(prefix))
			if len(d) != 8 {
				continue
			}
			var val float64
			if kind == "marine" {
				var pt DailyMarinePoint
				if err := json.Unmarshal(v, &pt); err != nil {
					continue
				}
				val = extractDailyMarineMetric(pt, metric)
			} else {
				var pt DailyWeatherPoint
				if err := json.Unmarshal(v, &pt); err != nil {
					continue
				}
				val = extractDailyWeatherMetric(pt, metric)
			}
			if math.IsNaN(val) || math.IsInf(val, 0) {
				continue
			}
			samples = append(samples, rawMetricSample{ts: d + "0000", val: val})
		}
		return nil
	})
	return samples, earliestTS, latestTS, err
}

func chooseAutoGranularity(samples []rawMetricSample, fromDaily bool) string {
	if fromDaily {
		if len(samples) <= 180 {
			return "daily"
		}
		if len(samples) <= 1825 {
			return "monthly"
		}
		return "yearly"
	}
	if len(samples) <= 360 { // up to ~15 days hourly
		return "hourly"
	}
	if len(samples) <= 4500 { // up to ~187 days hourly
		return "daily"
	}
	return "monthly"
}

func aggregateSamples(samples []rawMetricSample, granularity string) []HistoryDataPoint {
	type aggBucket struct {
		ts    string
		label string
		sum   float64
		min   float64
		max   float64
		count int
	}

	var order []string
	buckets := make(map[string]*aggBucket)

	for _, s := range samples {
		var key, label, ts string
		switch granularity {
		case "yearly":
			key = s.ts[:4]
			ts = key + "01010000"
			label = key
		case "monthly":
			key = s.ts[:6]
			ts = key + "010000"
			label = fmt.Sprintf("%s-%s", s.ts[:4], s.ts[4:6])
		case "daily":
			key = s.ts[:8]
			ts = key + "0000"
			label = fmt.Sprintf("%s-%s-%s", s.ts[:4], s.ts[4:6], s.ts[6:8])
		default: // hourly
			key = s.ts[:10]
			ts = key + "00"
			label = fmt.Sprintf("%s/%s %s:00", s.ts[4:6], s.ts[6:8], s.ts[8:10])
		}

		b, exists := buckets[key]
		if !exists {
			b = &aggBucket{
				ts:    ts,
				label: label,
				min:   s.val,
				max:   s.val,
			}
			buckets[key] = b
			order = append(order, key)
		}
		b.sum += s.val
		if s.val < b.min {
			b.min = s.val
		}
		if s.val > b.max {
			b.max = s.val
		}
		b.count++
	}

	out := make([]HistoryDataPoint, 0, len(order))
	for _, k := range order {
		b := buckets[k]
		out = append(out, HistoryDataPoint{
			Timestamp: b.ts,
			Label:     b.label,
			Value:     round2(b.sum / float64(b.count)),
			Min:       round2(b.min),
			Max:       round2(b.max),
			Count:     b.count,
		})
	}
	return out
}

func applyMovingAverage(pts []HistoryDataPoint, window int) {
	if window <= 1 || len(pts) == 0 {
		return
	}
	sum := 0.0
	for i := range pts {
		sum += pts[i].Value
		if i >= window {
			sum -= pts[i-window].Value
		}
		denom := window
		if i+1 < window {
			denom = i + 1
		}
		avg := round2(sum / float64(denom))
		pts[i].MovingAvg = &avg
	}
}

func computeStats(samples []rawMetricSample) HistoryStats {
	n := len(samples)
	if n == 0 {
		return HistoryStats{}
	}

	sum := 0.0
	minVal := samples[0].val
	minTS := samples[0].ts
	maxVal := samples[0].val
	maxTS := samples[0].ts
	vals := make([]float64, n)

	for i, s := range samples {
		vals[i] = s.val
		sum += s.val
		if s.val < minVal {
			minVal = s.val
			minTS = s.ts
		}
		if s.val > maxVal {
			maxVal = s.val
			maxTS = s.ts
		}
	}
	mean := sum / float64(n)

	sort.Float64s(vals)
	median := vals[n/2]
	if n%2 == 0 {
		median = (vals[n/2-1] + vals[n/2]) / 2.0
	}

	sqDiff := 0.0
	for _, v := range vals {
		d := v - mean
		sqDiff += d * d
	}
	stdDev := math.Sqrt(sqDiff / float64(n))

	// Linear regression slope per day
	trendPerDay := 0.0
	if n >= 2 {
		t0, err0 := time.Parse("200601021504", samples[0].ts)
		if err0 == nil {
			var sx, sy, sxx, sxy float64
			valid := 0
			for _, s := range samples {
				ti, err := time.Parse("200601021504", s.ts)
				if err != nil {
					continue
				}
				x := ti.Sub(t0).Hours() / 24.0
				y := s.val
				sx += x
				sy += y
				sxx += x * x
				sxy += x * y
				valid++
			}
			if valid >= 2 {
				fn := float64(valid)
				denom := fn*sxx - sx*sx
				if math.Abs(denom) > 1e-9 {
					trendPerDay = (fn*sxy - sx*sy) / denom
				}
			}
		}
	}

	return HistoryStats{
		Count:        n,
		Mean:         round2(mean),
		Min:          round2(minVal),
		MinTimestamp: minTS,
		Max:          round2(maxVal),
		MaxTimestamp: maxTS,
		Median:       round2(median),
		StdDev:       round2(stdDev),
		Delta:        round2(samples[n-1].val - samples[0].val),
		TrendPerDay:  round4(trendPerDay),
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func quakeKey(q QuakeRecord) []byte {
	ts := q.OccurredAt.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return []byte(ts.Format("20060102T150405.000000000Z") + "_" + q.ID)
}

// SaveQuakeBatch stores multiple QuakeRecords in a single bbolt transaction.
func (s *Store) SaveQuakeBatch(items []QuakeRecord) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketQuakes)
		for _, q := range items {
			if q.ID == "" {
				continue
			}
			data, err := json.Marshal(q)
			if err != nil {
				return err
			}
			if err := b.Put(quakeKey(q), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListRecentQuakes returns up to limit most recent earthquakes (newest first).
func (s *Store) ListRecentQuakes(limit int) ([]QuakeRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []QuakeRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketQuakes).Cursor()
		for k, v := c.Last(); k != nil && len(out) < limit; k, v = c.Prev() {
			var q QuakeRecord
			if err := json.Unmarshal(v, &q); err != nil {
				continue
			}
			out = append(out, q)
		}
		return nil
	})
	return out, err
}

// GetQuakeBounds returns the earliest and latest OccurredAt timestamps and total count in bucketQuakes.
func (s *Store) GetQuakeBounds() (earliest, latest time.Time, count int, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketQuakes).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var q QuakeRecord
			if err := json.Unmarshal(v, &q); err != nil {
				continue
			}
			if earliest.IsZero() {
				earliest = q.OccurredAt
			}
			latest = q.OccurredAt
			count++
		}
		return nil
	})
	return earliest, latest, count, err
}

// QueryQuakeHistory aggregates stored earthquakes optionally filtered by distance from (lat, lon).
func (s *Store) QueryQuakeHistory(req QuakeHistoryQuery) (QuakeHistoryResult, error) {
	if req.ToTime.IsZero() {
		req.ToTime = time.Now().UTC()
	}
	if req.FromTime.IsZero() {
		req.FromTime = req.ToTime.Add(-365 * 24 * time.Hour)
	}

	var matched []QuakeRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketQuakes).Cursor()
		fromKey := []byte(req.FromTime.UTC().Format("20060102T150405"))
		for k, v := c.Seek(fromKey); k != nil; k, v = c.Next() {
			var q QuakeRecord
			if err := json.Unmarshal(v, &q); err != nil {
				continue
			}
			if q.OccurredAt.After(req.ToTime) {
				break
			}
			if q.OccurredAt.Before(req.FromTime) {
				continue
			}
			if req.RadiusKm > 0 {
				if q.Latitude == 0 && q.Longitude == 0 {
					continue
				}
				if haversineKm(req.Latitude, req.Longitude, q.Latitude, q.Longitude) > req.RadiusKm {
					continue
				}
			}
			matched = append(matched, q)
		}
		return nil
	})
	if err != nil {
		return QuakeHistoryResult{}, err
	}

	granularity := req.Granularity
	if granularity == "" || granularity == "auto" {
		if req.ToTime.Sub(req.FromTime) <= 45*24*time.Hour {
			granularity = "daily"
		} else {
			granularity = "monthly"
		}
	}

	res := QuakeHistoryResult{
		TotalCount:  len(matched),
		Granularity: granularity,
	}
	if len(matched) == 0 {
		return res, nil
	}

	type qBucket struct {
		label  string
		count  int
		maxMag float64
		sumMag float64
	}
	var order []string
	buckets := make(map[string]*qBucket)
	sumMag := 0.0
	maxScaleRank := 0

	for _, q := range matched {
		sumMag += q.Magnitude
		if q.Magnitude > res.MaxMagnitude {
			res.MaxMagnitude = round2(q.Magnitude)
		}
		if r := intensityRank(q.MaxScale); r > maxScaleRank {
			maxScaleRank = r
			res.MaxScale = q.MaxScale
		}

		var key string
		if granularity == "daily" {
			key = q.OccurredAt.UTC().Format("2006-01-02")
		} else {
			key = q.OccurredAt.UTC().Format("2006-01")
		}
		b, exists := buckets[key]
		if !exists {
			b = &qBucket{label: key}
			buckets[key] = b
			order = append(order, key)
		}
		b.count++
		b.sumMag += q.Magnitude
		if q.Magnitude > b.maxMag {
			b.maxMag = q.Magnitude
		}
	}
	res.AvgMagnitude = round2(sumMag / float64(len(matched)))
	for _, k := range order {
		b := buckets[k]
		res.Buckets = append(res.Buckets, QuakeBucketPoint{
			Label:        b.label,
			Count:        b.count,
			MaxMagnitude: round2(b.maxMag),
			AvgMagnitude: round2(b.sumMag / float64(b.count)),
		})
	}

	// Recent up to 50 (newest first)
	for i := len(matched) - 1; i >= 0 && len(res.Recent) < 50; i-- {
		res.Recent = append(res.Recent, matched[i])
	}
	return res, nil
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func intensityRank(s string) int {
	switch strings.TrimSpace(s) {
	case "1":
		return 10
	case "2":
		return 20
	case "3":
		return 30
	case "4":
		return 40
	case "5-", "5弱":
		return 45
	case "5+", "5強":
		return 50
	case "6-", "6弱":
		return 55
	case "6+", "6強":
		return 60
	case "7":
		return 70
	default:
		return 0
	}
}
