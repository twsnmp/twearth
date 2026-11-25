package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCorrelationCacheAndDiscoveryState(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	e1 := CorrelationCacheEntry{
		Key:         "k1",
		RequestJSON: json.RawMessage(`{"algorithm":"pearson"}`),
		MetricsJSON: json.RawMessage(`{"correlation":0.65}`),
		Algorithm:   "pearson",
		Granularity: "monthly",
		SampleCount: 48,
		Score:       0.65,
		UpdatedAt:   time.Now().UTC(),
	}
	e2 := CorrelationCacheEntry{
		Key:         "k2",
		RequestJSON: json.RawMessage(`{"algorithm":"spearman"}`),
		MetricsJSON: json.RawMessage(`{"correlation":-0.82}`),
		Algorithm:   "spearman",
		Granularity: "monthly",
		SampleCount: 48,
		Score:       0.82,
		UpdatedAt:   time.Now().UTC(),
	}

	if err := st.SaveCorrelationCacheEntry(e1); err != nil {
		t.Fatalf("save e1: %v", err)
	}
	if err := st.SaveCorrelationCacheEntry(e2); err != nil {
		t.Fatalf("save e2: %v", err)
	}

	got, ok, err := st.GetCorrelationCacheEntry("k1")
	if err != nil || !ok || got.Score != 0.65 {
		t.Fatalf("get k1 failed: ok=%v err=%v got=%+v", ok, err, got)
	}

	list, err := st.ListCorrelationCacheEntries()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Key != "k2" || list[1].Key != "k1" {
		t.Fatalf("expected [k2, k1] sorted by score desc, got %+v", list)
	}

	deleted, err := st.DeleteCorrelationCacheEntries([]string{"k1"})
	if err != nil || deleted != 1 {
		t.Fatalf("delete k1: deleted=%d err=%v", deleted, err)
	}
	list, _ = st.ListCorrelationCacheEntries()
	if len(list) != 1 || list[0].Key != "k2" {
		t.Fatalf("expected only k2 remaining, got %+v", list)
	}

	if err := st.ClearCorrelationCache(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	list, _ = st.ListCorrelationCacheEntries()
	if len(list) != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", len(list))
	}

	state := DiscoverySavedState{
		Status:     "paused",
		ConfigJSON: json.RawMessage(`{"rangeKey":"5y"}`),
		Total:      10,
		Cursor:     4,
		Computed:   3,
		CacheHits:  1,
		UpdatedAt:  time.Now().UTC(),
	}
	if err := st.SaveDiscoveryState(state); err != nil {
		t.Fatalf("save discovery state: %v", err)
	}
	loaded, ok, err := st.LoadDiscoveryState()
	if err != nil || !ok || loaded.Status != "paused" || loaded.Cursor != 4 {
		t.Fatalf("load discovery state failed: ok=%v err=%v loaded=%+v", ok, err, loaded)
	}
}
