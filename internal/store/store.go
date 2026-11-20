package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketConfig     = []byte("config")
	bucketTimeSeries = []byte("timeseries")
	bucketEvents     = []byte("events")
	configKey        = []byte("main")
)

// ObservationLocation represents a registered land/city monitoring point.
type ObservationLocation struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	NameEn    string  `json:"nameEn,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Source    string  `json:"source"` // "preset" | "current" | "search" | "custom"
	IsPrimary bool    `json:"isPrimary"`
	Order     int     `json:"order"`
}

// MarineLocation represents a registered ocean/coastal sea surface temperature monitoring point.
type MarineLocation struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	NameEn    string  `json:"nameEn,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Region    string  `json:"region,omitempty"`
	IsPrimary bool    `json:"isPrimary"`
	Order     int     `json:"order"`
}

// Config holds user preferences, observation locations, marine locations, and alert thresholds.
type Config struct {
	Language string `json:"language"` // "auto" | "ja" | "en"

	Locations       []ObservationLocation `json:"locations"`
	MarineLocations []MarineLocation      `json:"marineLocations"`

	// Legacy single-location fields kept only for JSON migration from older versions.
	LegacyLocationName string  `json:"locationName,omitempty"`
	LegacyLatitude     float64 `json:"latitude,omitempty"`
	LegacyLongitude    float64 `json:"longitude,omitempty"`

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

	// App behavior
	AutoCheckUpdate  bool `json:"autoCheckUpdate"`
	MobileLightMode  bool `json:"mobileLightMode"`
	EventWindowHours int  `json:"eventWindowHours"` // -1 means all history; 0 defaults to 168h (7 days)
}

// DefaultLocations returns the initial city observation locations.
func DefaultLocations() []ObservationLocation {
	return []ObservationLocation{
		{
			ID:        "tokyo",
			Name:      "東京",
			NameEn:    "Tokyo",
			Latitude:  35.6762,
			Longitude: 139.6503,
			Source:    "preset",
			IsPrimary: true,
			Order:     0,
		},
	}
}

// DefaultMarineLocations returns the initial marine observation locations.
func DefaultMarineLocations() []MarineLocation {
	return []MarineLocation{
		{
			ID:        "sagami-bay",
			Name:      "相模湾",
			NameEn:    "Sagami Bay",
			Latitude:  35.15,
			Longitude: 139.35,
			Region:    "Pacific",
			IsPrimary: true,
			Order:     0,
		},
		{
			ID:        "kuroshio-shionomisaki",
			Name:      "潮岬沖 (黒潮域)",
			NameEn:    "Off Shionomisaki (Kuroshio)",
			Latitude:  33.25,
			Longitude: 135.75,
			Region:    "Pacific",
			IsPrimary: false,
			Order:     1,
		},
	}
}

// DefaultConfig returns sensible defaults for standalone operation.
func DefaultConfig() Config {
	return Config{
		Language:            "auto",
		Locations:           DefaultLocations(),
		MarineLocations:     DefaultMarineLocations(),
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
		AutoCheckUpdate:     true,
		MobileLightMode:     true,
		EventWindowHours:    168,
	}
}

// Normalize ensures Locations and MarineLocations have valid IDs, orders, and a single primary entry.
func (c *Config) Normalize() {
	if c.Language == "" {
		c.Language = "auto"
	}

	if len(c.Locations) == 0 {
		if c.LegacyLatitude != 0 || c.LegacyLongitude != 0 {
			name := strings.TrimSpace(c.LegacyLocationName)
			if name == "" {
				name = "Home"
			}
			c.Locations = []ObservationLocation{
				{
					ID:        "migrated-home",
					Name:      name,
					NameEn:    name,
					Latitude:  c.LegacyLatitude,
					Longitude: c.LegacyLongitude,
					Source:    "custom",
					IsPrimary: true,
					Order:     0,
				},
			}
		} else {
			c.Locations = DefaultLocations()
		}
	}
	c.LegacyLocationName = ""
	c.LegacyLatitude = 0
	c.LegacyLongitude = 0

	hasPrimaryLoc := false
	for i := range c.Locations {
		if c.Locations[i].ID == "" {
			c.Locations[i].ID = fmt.Sprintf("loc-%d", i+1)
		}
		if c.Locations[i].Name == "" {
			c.Locations[i].Name = c.Locations[i].NameEn
		}
		if c.Locations[i].NameEn == "" {
			c.Locations[i].NameEn = c.Locations[i].Name
		}
		if c.Locations[i].Source == "" {
			c.Locations[i].Source = "custom"
		}
		c.Locations[i].Order = i
		if c.Locations[i].IsPrimary {
			if hasPrimaryLoc {
				c.Locations[i].IsPrimary = false
			}
			hasPrimaryLoc = true
		}
	}
	if !hasPrimaryLoc && len(c.Locations) > 0 {
		c.Locations[0].IsPrimary = true
	}

	if len(c.MarineLocations) == 0 {
		c.MarineLocations = DefaultMarineLocations()
	}
	hasPrimaryMar := false
	for i := range c.MarineLocations {
		if c.MarineLocations[i].ID == "" {
			c.MarineLocations[i].ID = fmt.Sprintf("marine-%d", i+1)
		}
		if c.MarineLocations[i].Name == "" {
			c.MarineLocations[i].Name = c.MarineLocations[i].NameEn
		}
		if c.MarineLocations[i].NameEn == "" {
			c.MarineLocations[i].NameEn = c.MarineLocations[i].Name
		}
		c.MarineLocations[i].Order = i
		if c.MarineLocations[i].IsPrimary {
			if hasPrimaryMar {
				c.MarineLocations[i].IsPrimary = false
			}
			hasPrimaryMar = true
		}
	}
	if !hasPrimaryMar && len(c.MarineLocations) > 0 {
		c.MarineLocations[0].IsPrimary = true
	}

	if c.WeatherIntervalMin <= 0 {
		c.WeatherIntervalMin = 15
	}
	if c.WindGridIntervalMin <= 0 {
		c.WindGridIntervalMin = 60
	}
	if c.MarineIntervalMin <= 0 {
		c.MarineIntervalMin = 60
	}
	if c.VolcanoIntervalMin <= 0 {
		c.VolcanoIntervalMin = 15
	}
	if c.LidenIntervalMin <= 0 {
		c.LidenIntervalMin = 5
	}
	if c.SpaceIntervalMin <= 0 {
		c.SpaceIntervalMin = 15
	}
	if c.LightningRadiusKm <= 0 {
		c.LightningRadiusKm = 20
	}
	if c.QuakeMinIntensity == "" {
		c.QuakeMinIntensity = "3"
	}
	if c.PressureDropHpa3h <= 0 {
		c.PressureDropHpa3h = 4.0
	}
	if c.KpIndexThreshold <= 0 {
		c.KpIndexThreshold = 5.0
	}
	if c.EventWindowHours == 0 {
		c.EventWindowHours = 168
	}
}

// PrimaryLocation returns the active primary land location.
func (c Config) PrimaryLocation() ObservationLocation {
	for _, loc := range c.Locations {
		if loc.IsPrimary {
			return loc
		}
	}
	if len(c.Locations) > 0 {
		return c.Locations[0]
	}
	return DefaultLocations()[0]
}

// PrimaryMarineLocation returns the active primary marine location.
func (c Config) PrimaryMarineLocation() MarineLocation {
	for _, loc := range c.MarineLocations {
		if loc.IsPrimary {
			return loc
		}
	}
	if len(c.MarineLocations) > 0 {
		return c.MarineLocations[0]
	}
	return DefaultMarineLocations()[0]
}

// TimeSeriesPoint represents a local observation snapshot keyed by YYYYMMDDHHmm.
type TimeSeriesPoint struct {
	LocationID     string  `json:"locationId,omitempty"`
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
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Severity   string            `json:"severity"`
	LocationID string            `json:"locationId,omitempty"`
	Title      string            `json:"title"`
	Detail     string            `json:"detail"`
	TitleKey   string            `json:"titleKey,omitempty"`
	DetailKey  string            `json:"detailKey,omitempty"`
	Params     map[string]string `json:"params,omitempty"`
	Latitude   float64           `json:"latitude,omitempty"`
	Longitude  float64           `json:"longitude,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
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
		// Reset slices before unmarshaling so saved empty slices don't merge with defaults.
		cfg.Locations = nil
		cfg.MarineLocations = nil
		return json.Unmarshal(raw, &cfg)
	})
	cfg.Normalize()
	return cfg, err
}

// SaveConfig normalizes and persists cfg to bbolt.
func (s *Store) SaveConfig(cfg Config) error {
	cfg.Normalize()
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketConfig).Put(configKey, data)
	})
}

// SaveTimeSeries stores a snapshot keyed by YYYYMMDDHHmm (or locationId:YYYYMMDDHHmm).
func (s *Store) SaveTimeSeries(pt TimeSeriesPoint) error {
	if pt.Timestamp == "" {
		pt.Timestamp = time.Now().UTC().Format("200601021504")
	}
	key := pt.Timestamp
	if pt.LocationID != "" {
		key = pt.LocationID + ":" + pt.Timestamp
	}
	data, err := json.Marshal(pt)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTimeSeries).Put([]byte(key), data)
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

// ListEventsSince returns events at or after since (newest first), up to limit items.
// If since is zero, all stored events up to limit are returned.
func (s *Store) ListEventsSince(since time.Time, limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 1000
	}
	var out []EventRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketEvents).Cursor()
		for k, v := c.Last(); k != nil && len(out) < limit; k, v = c.Prev() {
			var ev EventRecord
			if err := json.Unmarshal(v, &ev); err != nil {
				return err
			}
			if !since.IsZero() && !ev.Timestamp.IsZero() && ev.Timestamp.Before(since) {
				break
			}
			out = append(out, ev)
		}
		return nil
	})
	return out, err
}
