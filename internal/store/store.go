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
	bucketConfig           = []byte("config")
	bucketTimeSeries       = []byte("timeseries")
	bucketMarineTimeSeries = []byte("marine_timeseries")
	bucketDailyTimeSeries  = []byte("daily_timeseries")
	bucketDailyMarine      = []byte("daily_marine")
	bucketHistorySync      = []byte("history_sync")
	bucketQuakes           = []byte("quakes")
	bucketEvents           = []byte("events")
	bucketDisasters        = []byte("disasters")
	bucketDisasterTimeIdx  = []byte("disaster_time_idx")
	bucketRiverTimeSeries  = []byte("river_timeseries")
	bucketDisasterLore     = []byte("disaster_lore")
	bucketFeedCursors      = []byte("feed_cursors")
	configKey              = []byte("main")
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
	Source    string  `json:"source,omitempty"`   // "auto" | "preset" | "custom"
	Category  string  `json:"category,omitempty"` // "nearshore" | "regional" | "climate"
	IsPrimary bool    `json:"isPrimary"`
	Order     int     `json:"order"`
}

// Config holds user preferences, observation locations, marine locations, and alert thresholds.
type Config struct {
	Language string `json:"language"` // "auto" | "ja" | "en"

	Locations           []ObservationLocation `json:"locations"`
	MarineLocations     []MarineLocation      `json:"marineLocations"`
	AutoMarineByPrimary bool                  `json:"autoMarineByPrimary"`
	AutoMarineClimate   bool                  `json:"autoMarineClimate"`
	MarineAutoTargetKey string                `json:"marineAutoTargetKey,omitempty"`
	MarineConfigVersion int                   `json:"marineConfigVersion,omitempty"`

	// Legacy single-location fields kept only for JSON migration from older versions.
	LegacyLocationName string  `json:"locationName,omitempty"`
	LegacyLatitude     float64 `json:"latitude,omitempty"`
	LegacyLongitude    float64 `json:"longitude,omitempty"`

	// Polling intervals (minutes)
	WeatherIntervalMin        int `json:"weatherIntervalMin"`
	WindGridIntervalMin       int `json:"windGridIntervalMin"`
	MarineIntervalMin         int `json:"marineIntervalMin"`
	VolcanoIntervalMin        int `json:"volcanoIntervalMin"`
	LidenIntervalMin          int `json:"lidenIntervalMin"`
	SpaceIntervalMin          int `json:"spaceIntervalMin"`
	JmaExtraIntervalMin       int `json:"jmaExtraIntervalMin"`
	KikikuruIntervalMin       int `json:"kikikuruIntervalMin"`
	RiverIntervalMin          int `json:"riverIntervalMin"`
	GlobalDisasterIntervalMin int `json:"globalDisasterIntervalMin"`

	// Alert thresholds
	LightningRadiusKm   float64 `json:"lightningRadiusKm"`
	QuakeMinIntensity   string  `json:"quakeMinIntensity"`
	PressureDropHpa3h   float64 `json:"pressureDropHpa3h"`
	KpIndexThreshold    float64 `json:"kpIndexThreshold"`
	RainAlertMm         float64 `json:"rainAlertMm"`
	SnowfallAlertCm     float64 `json:"snowfallAlertCm"`
	RiverRadiusKm       float64 `json:"riverRadiusKm"`
	AlertDistanceMode   string  `json:"alertDistanceMode,omitempty"`   // "nearest" | "primary"
	AlertNearRadiusKm   float64 `json:"alertNearRadiusKm,omitempty"`   // full severity radius (default 100km)
	AlertMediumRadiusKm float64 `json:"alertMediumRadiusKm,omitempty"` // 1-step attenuation radius (default 300km)

	// App behavior
	AutoCheckUpdate       bool `json:"autoCheckUpdate"`
	MobileLightMode       bool `json:"mobileLightMode"`
	EventWindowHours      int  `json:"eventWindowHours"`   // -1 means all history; 0 defaults to 168h (7 days)
	HistoryDays           int  `json:"historyDays"`        // -1 means all available; 0 defaults to 365 days
	HourlyHistoryDays     int  `json:"hourlyHistoryDays"`  // 0 defaults to 90 days (capped at 365)
	DisableHistorySync    bool `json:"disableHistorySync,omitempty"`
	DisableJmaExtra       bool `json:"disableJmaExtra,omitempty"`
	DisableKikikuru       bool `json:"disableKikikuru,omitempty"`
	DisableRiver          bool `json:"disableRiver,omitempty"`
	DisableGlobalDisaster bool `json:"disableGlobalDisaster,omitempty"`
	DisableDisasterLore   bool `json:"disableDisasterLore,omitempty"`
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

// DefaultMarineLocations returns the initial marine observation locations tailored to the default city.
func DefaultMarineLocations() []MarineLocation {
	return RecommendMarineLocations(DefaultLocations()[0], true)
}

// DefaultConfig returns sensible defaults for standalone operation.
func DefaultConfig() Config {
	defaultLocs := DefaultLocations()
	return Config{
		Language:                  "auto",
		Locations:                 defaultLocs,
		MarineLocations:           RecommendMarineLocations(defaultLocs[0], true),
		AutoMarineByPrimary:       true,
		AutoMarineClimate:         true,
		MarineAutoTargetKey:       marineAutoKey(defaultLocs[0], true),
		MarineConfigVersion:       1,
		WeatherIntervalMin:        15,
		WindGridIntervalMin:       60,
		MarineIntervalMin:         60,
		VolcanoIntervalMin:        15,
		LidenIntervalMin:          5,
		SpaceIntervalMin:          15,
		JmaExtraIntervalMin:       5,
		KikikuruIntervalMin:       10,
		RiverIntervalMin:          15,
		GlobalDisasterIntervalMin: 20,
		LightningRadiusKm:         20,
		QuakeMinIntensity:         "3",
		PressureDropHpa3h:         4.0,
		KpIndexThreshold:          5.0,
		RainAlertMm:               20.0,
		SnowfallAlertCm:           5.0,
		RiverRadiusKm:             30.0,
		AlertDistanceMode:         "nearest",
		AlertNearRadiusKm:         100.0,
		AlertMediumRadiusKm:       300.0,
		AutoCheckUpdate:           true,
		MobileLightMode:           true,
		EventWindowHours:          168,
		HistoryDays:               365,
		HourlyHistoryDays:         90,
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

	primaryLoc := c.PrimaryLocation()
	targetKey := marineAutoKey(primaryLoc, c.AutoMarineClimate)
	if c.AutoMarineByPrimary && (c.MarineAutoTargetKey != targetKey || len(c.MarineLocations) == 0) {
		c.MarineLocations = MergeAutoMarineLocations(c.MarineLocations, primaryLoc, c.AutoMarineClimate)
		c.MarineAutoTargetKey = targetKey
	} else if len(c.MarineLocations) == 0 {
		c.MarineLocations = RecommendMarineLocations(primaryLoc, c.AutoMarineClimate)
	}
	if c.MarineConfigVersion < 1 {
		c.MarineConfigVersion = 1
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
		if c.MarineLocations[i].Source == "" {
			c.MarineLocations[i].Source = "custom"
		}
		if c.MarineLocations[i].Category == "" {
			if e, ok := findCatalogEntry(c.MarineLocations[i].ID); ok {
				c.MarineLocations[i].Category = e.Category
			} else {
				c.MarineLocations[i].Category = "regional"
			}
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
	if c.JmaExtraIntervalMin <= 0 {
		c.JmaExtraIntervalMin = 5
	}
	if c.KikikuruIntervalMin <= 0 {
		c.KikikuruIntervalMin = 10
	}
	if c.RiverIntervalMin <= 0 {
		c.RiverIntervalMin = 15
	}
	if c.GlobalDisasterIntervalMin <= 0 {
		c.GlobalDisasterIntervalMin = 20
	}
	if c.LightningRadiusKm <= 0 {
		c.LightningRadiusKm = 20
	}
	if c.RiverRadiusKm <= 0 {
		c.RiverRadiusKm = 30.0
	}
	if c.AlertDistanceMode != "primary" {
		c.AlertDistanceMode = "nearest"
	}
	if c.AlertNearRadiusKm <= 0 {
		c.AlertNearRadiusKm = 100.0
	}
	if c.AlertMediumRadiusKm <= 0 {
		c.AlertMediumRadiusKm = 300.0
	}
	if c.AlertMediumRadiusKm < c.AlertNearRadiusKm {
		c.AlertMediumRadiusKm = c.AlertNearRadiusKm
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
	if c.RainAlertMm <= 0 {
		c.RainAlertMm = 20.0
	}
	if c.SnowfallAlertCm <= 0 {
		c.SnowfallAlertCm = 5.0
	}
	if c.EventWindowHours == 0 {
		c.EventWindowHours = 168
	}
	if c.HistoryDays == 0 || c.HistoryDays < -1 {
		c.HistoryDays = 365
	}
	if c.HourlyHistoryDays <= 0 {
		c.HourlyHistoryDays = 90
	}
	if c.HourlyHistoryDays > 365 {
		c.HourlyHistoryDays = 365
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
	Precipitation  float64 `json:"precipitation"`
	Rain           float64 `json:"rain"`
	Snowfall       float64 `json:"snowfall"`
	SnowDepth      float64 `json:"snowDepth"`
	CAPE           float64 `json:"cape"`
	SeaSurfaceTemp float64 `json:"seaSurfaceTemp"`
	LightningCount int     `json:"lightningCount"`
	KpIndex        float64 `json:"kpIndex"`
}

// EventRecord represents a detected natural/disaster event or alert.
type EventRecord struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Severity     string            `json:"severity"`
	BaseSeverity string            `json:"baseSeverity,omitempty"`
	LocationID   string            `json:"locationId,omitempty"`
	Title        string            `json:"title"`
	Detail       string            `json:"detail"`
	TitleKey     string            `json:"titleKey,omitempty"`
	DetailKey    string            `json:"detailKey,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	Latitude     float64           `json:"latitude,omitempty"`
	Longitude    float64           `json:"longitude,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
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
		for _, b := range [][]byte{
			bucketConfig,
			bucketTimeSeries,
			bucketMarineTimeSeries,
			bucketDailyTimeSeries,
			bucketDailyMarine,
			bucketHistorySync,
			bucketQuakes,
			bucketEvents,
			bucketDisasters,
			bucketDisasterTimeIdx,
			bucketRiverTimeSeries,
			bucketDisasterLore,
			bucketFeedCursors,
		} {
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

		// One-time migration for legacy marine config: discard old marine history and
		// re-seed marine locations based on the active primary city + El Nino regions.
		if raw := tx.Bucket(bucketConfig).Get(configKey); raw != nil {
			var ver struct {
				MarineConfigVersion int `json:"marineConfigVersion"`
			}
			if err := json.Unmarshal(raw, &ver); err == nil && ver.MarineConfigVersion < 1 {
				if err := clearMarineHistoryTx(tx); err != nil {
					return err
				}
				cfg := DefaultConfig()
				cfg.Locations = nil
				cfg.MarineLocations = nil
				cfg.MarineConfigVersion = 0
				if err := json.Unmarshal(raw, &cfg); err == nil {
					cfg.AutoMarineByPrimary = true
					cfg.AutoMarineClimate = true
					cfg.MarineConfigVersion = 1
					cfg.MarineLocations = nil
					cfg.MarineAutoTargetKey = ""
					cfg.Normalize()
					if nextRaw, mErr := json.Marshal(cfg); mErr == nil {
						if err := tx.Bucket(bucketConfig).Put(configKey, nextRaw); err != nil {
							return err
						}
					}
				}
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
		// Reset slices and version before unmarshaling so saved empty slices/legacy configs migrate cleanly.
		cfg.Locations = nil
		cfg.MarineLocations = nil
		cfg.MarineConfigVersion = 0
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return err
		}
		if cfg.MarineConfigVersion < 1 {
			cfg.AutoMarineByPrimary = true
			cfg.AutoMarineClimate = true
			cfg.MarineConfigVersion = 1
			cfg.MarineLocations = nil
			cfg.MarineAutoTargetKey = ""
		}
		return nil
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

// RewriteEvents iterates over stored events in a single transaction, updating or deleting entries.
// Returning keep=false deletes the event; returning changed=true persists the updated record.
func (s *Store) RewriteEvents(fn func(ev EventRecord) (updated EventRecord, changed bool, keep bool)) error {
	if s == nil || s.db == nil || fn == nil {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEvents)
		type op struct {
			key  []byte
			data []byte
			del  bool
		}
		var ops []op
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var ev EventRecord
			if err := json.Unmarshal(v, &ev); err != nil {
				continue
			}
			next, changed, keep := fn(ev)
			if !keep {
				ops = append(ops, op{key: append([]byte(nil), k...), del: true})
				continue
			}
			if changed {
				raw, err := json.Marshal(next)
				if err != nil {
					return err
				}
				ops = append(ops, op{key: append([]byte(nil), k...), data: raw})
			}
		}
		for _, o := range ops {
			if o.del {
				if err := b.Delete(o.key); err != nil {
					return err
				}
			} else if err := b.Put(o.key, o.data); err != nil {
				return err
			}
		}
		return nil
	})
}
