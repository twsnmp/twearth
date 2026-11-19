package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketConfig     = []byte("config")
	bucketTimeSeries = []byte("timeseries")
	bucketEvents     = []byte("events")
	configKey        = []byte("main")
)

// Config holds user preferences, home coordinates, thresholds, and notification targets.
type Config struct {
	LocationName string  `json:"locationName"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`

	// Polling intervals (minutes)
	WeatherIntervalMin  int `json:"weatherIntervalMin"`
	WindGridIntervalMin int `json:"windGridIntervalMin"`
	MarineIntervalMin   int `json:"marineIntervalMin"`
	VolcanoIntervalMin  int `json:"volcanoIntervalMin"`
	LidenIntervalMin    int `json:"lidenIntervalMin"`
	SpaceIntervalMin    int `json:"spaceIntervalMin"`

	// Alert thresholds
	LightningRadiusKm float64 `json:"lightningRadiusKm"`
	QuakeMinIntensity string  `json:"quakeMinIntensity"`
	PressureDropHpa3h float64 `json:"pressureDropHpa3h"`
	KpIndexThreshold  float64 `json:"kpIndexThreshold"`

	// TWSNMP external notifiers
	SyslogEnabled  bool   `json:"syslogEnabled"`
	SyslogHost     string `json:"syslogHost"`
	SyslogPort     int    `json:"syslogPort"`
	MQTTEnabled    bool   `json:"mqttEnabled"`
	MQTTBroker     string `json:"mqttBroker"`
	MQTTTopic      string `json:"mqttTopic"`
	SNMPTrapEnabled bool  `json:"snmpTrapEnabled"`
	SNMPTrapTarget string `json:"snmpTrapTarget"`
	SNMPCommunity  string `json:"snmpCommunity"`
	WebhookEnabled bool   `json:"webhookEnabled"`
	WebhookURL     string `json:"webhookUrl"`
	NotifyOnMobile bool   `json:"notifyOnMobile"`

	// App behavior
	AutoCheckUpdate bool `json:"autoCheckUpdate"`
	MobileLightMode bool `json:"mobileLightMode"`
}

// DefaultConfig returns sensible defaults centered near Fujimi-shi, Saitama.
func DefaultConfig() Config {
	return Config{
		LocationName:        "自宅 (富士見市付近)",
		Latitude:            35.8569,
		Longitude:           139.5492,
		WeatherIntervalMin:  15,
		WindGridIntervalMin: 60,
		MarineIntervalMin:   60,
		VolcanoIntervalMin:  15,
		LidenIntervalMin:    5,
		SpaceIntervalMin:    15,
		LightningRadiusKm:   20,
		QuakeMinIntensity:   "3",
		PressureDropHpa3h:   4.0,
		KpIndexThreshold:    5.0,
		SyslogPort:          514,
		MQTTTopic:           "twsnmp/twearth/events",
		SNMPCommunity:       "public",
		NotifyOnMobile:      false,
		AutoCheckUpdate:     true,
		MobileLightMode:     true,
	}
}

// TimeSeriesPoint represents a local observation snapshot keyed by YYYYMMDDHHmm.
type TimeSeriesPoint struct {
	Timestamp      string  `json:"timestamp"`
	PressureMSL    float64 `json:"pressureMsl"`
	Temperature2m  float64 `json:"temperature2m"`
	CAPE           float64 `json:"cape"`
	SeaSurfaceTemp float64 `json:"seaSurfaceTemp"`
	LightningCount int     `json:"lightningCount"`
	KpIndex        float64 `json:"kpIndex"`
}

// EventRecord represents a detected natural/disaster event or alert.
type EventRecord struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Latitude  float64   `json:"latitude,omitempty"`
	Longitude float64   `json:"longitude,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Store wraps bbolt operations for configuration, time series, and event history.
type Store struct {
	db *bolt.DB
}

// Open creates the data directory if needed and opens the bbolt database.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "twearth.db")
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketConfig, bucketTimeSeries, bucketEvents} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}

		// Purge corrupted future-dated events (e.g., JST parsed as UTC in older builds).
		cutoff := time.Now().UTC().Add(5 * time.Minute)
		evBucket := tx.Bucket(bucketEvents)
		c := evBucket.Cursor()
		var toDelete [][]byte
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var ev EventRecord
			if err := json.Unmarshal(v, &ev); err != nil {
				continue
			}
			if !ev.Timestamp.After(cutoff) {
				break
			}
			toDelete = append(toDelete, append([]byte(nil), k...))
		}
		for _, k := range toDelete {
			if err := evBucket.Delete(k); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying bbolt database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// GetConfig returns the stored Config, or DefaultConfig if none has been saved yet.
func (s *Store) GetConfig() (Config, error) {
	cfg := DefaultConfig()
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketConfig).Get(configKey)
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, &cfg)
	})
	return cfg, err
}

// SaveConfig persists cfg to bbolt.
func (s *Store) SaveConfig(cfg Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketConfig).Put(configKey, data)
	})
}

// SaveTimeSeries stores a snapshot keyed by YYYYMMDDHHmm.
func (s *Store) SaveTimeSeries(pt TimeSeriesPoint) error {
	if pt.Timestamp == "" {
		pt.Timestamp = time.Now().UTC().Format("200601021504")
	}
	data, err := json.Marshal(pt)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTimeSeries).Put([]byte(pt.Timestamp), data)
	})
}

// ListTimeSeries returns snapshots whose keys fall within [fromKey, toKey].
func (s *Store) ListTimeSeries(fromKey, toKey string) ([]TimeSeriesPoint, error) {
	var out []TimeSeriesPoint
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketTimeSeries).Cursor()
		for k, v := c.Seek([]byte(fromKey)); k != nil && string(k) <= toKey; k, v = c.Next() {
			var pt TimeSeriesPoint
			if err := json.Unmarshal(v, &pt); err != nil {
				return err
			}
			out = append(out, pt)
		}
		return nil
	})
	return out, err
}

// SaveEvent stores an event keyed by RFC3339Nano timestamp + ID for chronological ordering.
func (s *Store) SaveEvent(ev EventRecord) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	key := ev.Timestamp.UTC().Format("20060102T150405.000000000Z") + "_" + ev.ID
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketEvents).Put([]byte(key), data)
	})
}

// ListEvents returns up to limit most recent events (newest first).
func (s *Store) ListEvents(limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []EventRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketEvents).Cursor()
		for k, v := c.Last(); k != nil && len(out) < limit; k, v = c.Prev() {
			var ev EventRecord
			if err := json.Unmarshal(v, &ev); err != nil {
				return err
			}
			out = append(out, ev)
		}
		return nil
	})
	return out, err
}
