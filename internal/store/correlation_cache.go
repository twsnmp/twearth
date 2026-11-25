package store

import (
	"encoding/json"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var discoveryStateKey = []byte("current")

// CorrelationCacheEntry persists a lightweight summary of an evaluated correlation pair.
// Raw time-series arrays are intentionally omitted to keep storage compact across thousands of pairs.
type CorrelationCacheEntry struct {
	Key         string          `json:"key"`
	RequestJSON json.RawMessage `json:"requestJson"`
	MetricsJSON json.RawMessage `json:"metricsJson"`
	Algorithm   string          `json:"algorithm"`
	Granularity string          `json:"granularity"`
	SampleCount int             `json:"sampleCount"`
	Score       float64         `json:"score"`
	Warnings    []string        `json:"warnings,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// DiscoverySavedState persists the current correlation discovery job config and progress cursor
// so interrupted or paused discovery runs can be resumed across sessions.
type DiscoverySavedState struct {
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

// SaveCorrelationCacheEntry inserts or updates a correlation cache entry by key.
func (s *Store) SaveCorrelationCacheEntry(entry CorrelationCacheEntry) error {
	if s == nil || s.db == nil || entry.Key == "" {
		return nil
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCorrelationCache)
		return b.Put([]byte(entry.Key), data)
	})
}

// GetCorrelationCacheEntry retrieves a single cached correlation entry by key.
func (s *Store) GetCorrelationCacheEntry(key string) (CorrelationCacheEntry, bool, error) {
	var out CorrelationCacheEntry
	if s == nil || s.db == nil || key == "" {
		return out, false, nil
	}
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCorrelationCache)
		raw := b.Get([]byte(key))
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &out)
	})
	return out, found, err
}

// ListCorrelationCacheEntries returns all cached correlation entries sorted by Score descending.
func (s *Store) ListCorrelationCacheEntries() ([]CorrelationCacheEntry, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []CorrelationCacheEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCorrelationCache)
		return b.ForEach(func(_, v []byte) error {
			var entry CorrelationCacheEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return nil
			}
			out = append(out, entry)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// DeleteCorrelationCacheEntries removes specific cached entries by key.
func (s *Store) DeleteCorrelationCacheEntries(keys []string) (int, error) {
	if s == nil || s.db == nil || len(keys) == 0 {
		return 0, nil
	}
	deleted := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCorrelationCache)
		for _, k := range keys {
			if k == "" {
				continue
			}
			if b.Get([]byte(k)) != nil {
				if err := b.Delete([]byte(k)); err != nil {
					return err
				}
				deleted++
			}
		}
		return nil
	})
	return deleted, err
}

// ClearCorrelationCache deletes all cached correlation entries.
func (s *Store) ClearCorrelationCache() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bucketCorrelationCache); err != nil && err != bolt.ErrBucketNotFound {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(bucketCorrelationCache)
		return err
	})
}

// SaveDiscoveryState persists the current discovery job progress and configuration.
func (s *Store) SaveDiscoveryState(st DiscoverySavedState) error {
	if s == nil || s.db == nil {
		return nil
	}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCorrelationDiscovery)
		return b.Put(discoveryStateKey, data)
	})
}

// LoadDiscoveryState loads the persisted discovery job state, if any.
func (s *Store) LoadDiscoveryState() (DiscoverySavedState, bool, error) {
	var out DiscoverySavedState
	if s == nil || s.db == nil {
		return out, false, nil
	}
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCorrelationDiscovery)
		raw := b.Get(discoveryStateKey)
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &out)
	})
	return out, found, err
}
