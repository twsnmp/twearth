package store

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// DailyWeatherPoint represents a daily climate summary keyed by locationId:YYYYMMDD.
type DailyWeatherPoint struct {
	LocationID       string  `json:"locationId"`
	Date             string  `json:"date"` // YYYYMMDD
	TempMean         float64 `json:"tempMean"`
	TempMax          float64 `json:"tempMax"`
	TempMin          float64 `json:"tempMin"`
	PressureMSLMean  float64 `json:"pressureMslMean"`
	PrecipitationSum float64 `json:"precipitationSum"`
	SnowfallSum      float64 `json:"snowfallSum"`
	WindSpeedMax     float64 `json:"windSpeedMax"`
}

// DailyMarinePoint represents a daily marine summary keyed by locationId:YYYYMMDD.
type DailyMarinePoint struct {
	LocationID          string  `json:"locationId"`
	Date                string  `json:"date"` // YYYYMMDD
	SSTMean             float64 `json:"sstMean"`
	SSTMin              float64 `json:"sstMin"`
	SSTMax              float64 `json:"sstMax"`
	WaveHeightMax       float64 `json:"waveHeightMax"`
	CurrentVelocityMean float64 `json:"currentVelocityMean"`
	MissingSST          bool    `json:"missingSst,omitempty"`
	MissingWave         bool    `json:"missingWave,omitempty"`
	MissingCurrent      bool    `json:"missingCurrent,omitempty"`
}

// SaveDailyWeatherBatch stores multiple DailyWeatherPoints in a single transaction.
func (s *Store) SaveDailyWeatherBatch(pts []DailyWeatherPoint) error {
	if len(pts) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDailyTimeSeries)
		for _, pt := range pts {
			if pt.LocationID == "" || len(pt.Date) != 8 {
				continue
			}
			key := pt.LocationID + ":" + pt.Date
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

// SaveDailyMarineBatch stores multiple DailyMarinePoints in a single transaction.
func (s *Store) SaveDailyMarineBatch(pts []DailyMarinePoint) error {
	if len(pts) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDailyMarine)
		for _, pt := range pts {
			if pt.LocationID == "" || len(pt.Date) != 8 {
				continue
			}
			key := pt.LocationID + ":" + pt.Date
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

// GetDailyBounds returns the earliest and latest YYYYMMDD dates and point count for a location.
func (s *Store) GetDailyBounds(kind, locationID string) (earliestDate, latestDate string, count int, err error) {
	if locationID == "" {
		return "", "", 0, nil
	}
	bucketName := bucketDailyTimeSeries
	if kind == "marine" {
		bucketName = bucketDailyMarine
	}
	prefix := []byte(locationID + ":")
	err = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketName).Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			d := strings.TrimPrefix(string(k), string(prefix))
			if earliestDate == "" {
				earliestDate = d
			}
			latestDate = d
			count++
		}
		return nil
	})
	return earliestDate, latestDate, count, err
}

func extractDailyWeatherMetric(pt DailyWeatherPoint, metric string) float64 {
	switch metric {
	case "pressureMsl":
		return pt.PressureMSLMean
	case "precipitation", "rain":
		return pt.PrecipitationSum
	case "snowfall":
		return pt.SnowfallSum
	case "windSpeed10m":
		return pt.WindSpeedMax
	default:
		return pt.TempMean
	}
}

func extractDailyMarineMetric(pt DailyMarinePoint, metric string) float64 {
	switch metric {
	case "waveHeight":
		if pt.MissingWave {
			return math.NaN()
		}
		return pt.WaveHeightMax
	case "oceanCurrentVelocity":
		if pt.MissingCurrent {
			return math.NaN()
		}
		return pt.CurrentVelocityMean
	default:
		if pt.MissingSST {
			return math.NaN()
		}
		return pt.SSTMean
	}
}

func recreateBucketTx(tx *bolt.Tx, name []byte) error {
	if tx.Bucket(name) != nil {
		if err := tx.DeleteBucket(name); err != nil {
			return err
		}
	}
	_, err := tx.CreateBucketIfNotExists(name)
	return err
}

func deleteSyncPrefixTx(tx *bolt.Tx, prefixes ...string) error {
	b := tx.Bucket(bucketHistorySync)
	if b == nil {
		return nil
	}
	var keys [][]byte
	c := b.Cursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		ks := string(k)
		for _, p := range prefixes {
			if strings.HasPrefix(ks, p) {
				keys = append(keys, append([]byte(nil), k...))
				break
			}
		}
	}
	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

func clearMarineHistoryTx(tx *bolt.Tx) error {
	if err := recreateBucketTx(tx, bucketMarineTimeSeries); err != nil {
		return err
	}
	if err := recreateBucketTx(tx, bucketDailyMarine); err != nil {
		return err
	}
	return deleteSyncPrefixTx(tx, "marine:", "marine_daily:")
}

// ClearHistoryData deletes stored historical data for the given scope ("all" | "weather" | "marine" | "quake")
// while preserving user configuration and alert event logs.
func (s *Store) ClearHistoryData(scope string) error {
	if scope == "" {
		scope = "all"
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		switch scope {
		case "weather":
			if err := recreateBucketTx(tx, bucketTimeSeries); err != nil {
				return err
			}
			if err := recreateBucketTx(tx, bucketDailyTimeSeries); err != nil {
				return err
			}
			return deleteSyncPrefixTx(tx, "weather:", "weather_daily:")
		case "marine":
			return clearMarineHistoryTx(tx)
		case "quake":
			if err := recreateBucketTx(tx, bucketQuakes); err != nil {
				return err
			}
			return deleteSyncPrefixTx(tx, "quake:")
		default: // "all"
			for _, b := range [][]byte{
				bucketTimeSeries,
				bucketMarineTimeSeries,
				bucketDailyTimeSeries,
				bucketDailyMarine,
				bucketQuakes,
			} {
				if err := recreateBucketTx(tx, b); err != nil {
					return err
				}
			}
			return deleteSyncPrefixTx(tx, "weather:", "weather_daily:", "marine:", "marine_daily:", "quake:")
		}
	})
}

func isLongRangeQuery(fromTS, toTS string) bool {
	if fromTS == "" {
		return true
	}
	if len(fromTS) < 8 || len(toTS) < 8 {
		return false
	}
	f, err1 := time.Parse("20060102", fromTS[:8])
	t, err2 := time.Parse("20060102", toTS[:8])
	if err1 != nil || err2 != nil {
		return false
	}
	return t.Sub(f) > 95*24*time.Hour
}
