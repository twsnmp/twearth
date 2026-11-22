package collector

import (
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

func TestMigrateAlertsIfNeeded(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	now := time.Now().UTC().Add(-time.Hour)
	// Seed legacy misclassified disasters
	err = st.SaveDisasterBatch([]store.DisasterRecord{
		{
			ID:        "adv-1",
			Source:    "jma_extra",
			Category:  "warning",
			Severity:  "critical",
			AlertLevel: "特別警報",
			Title:     "気象特別警報・警報・注意報 (宇都宮地方気象台)",
			Detail:    "【栃木県気象警報・注意報】栃木県では、空気の乾燥した状態が続くため、火の取り扱いに注意してください。",
			AreaName:  "宇都宮地方気象台",
			Latitude:  36.555,
			Longitude: 139.883,
			StartedAt: now,
			UpdatedAt: now,
			IsActive:  true,
		},
		{
			ID:        "cancel-1",
			Source:    "jma_extra",
			Category:  "warning",
			Severity:  "critical",
			AlertLevel: "特別警報",
			Title:     "気象特別警報・警報・注意報 (名瀬測候所)",
			Detail:    "【奄美地方（鹿児島県）気象警報・注意報】注意報を解除します。",
			AreaName:  "名瀬測候所",
			Latitude:  31.596,
			Longitude: 130.557,
			StartedAt: now.Add(time.Minute),
			UpdatedAt: now.Add(time.Minute),
			IsActive:  true,
		},
		{
			ID:        "warn-naze",
			Source:    "jma_extra",
			Category:  "warning",
			Severity:  "critical",
			AlertLevel: "特別警報",
			Title:     "気象特別警報・警報・注意報 (名瀬測候所)",
			Detail:    "【奄美地方（鹿児島県）気象警報・注意報】奄美地方では、暴風に警戒してください。",
			AreaName:  "名瀬測候所",
			Latitude:  31.596, // old Kagoshima mainland coord
			Longitude: 130.557,
			StartedAt: now.Add(2 * time.Minute),
			UpdatedAt: now.Add(2 * time.Minute),
			IsActive:  true,
		},
	})
	if err != nil {
		t.Fatalf("SaveDisasterBatch: %v", err)
	}

	// Seed legacy events
	for _, ev := range []store.EventRecord{
		{
			ID:        "dis-jma_extra-adv-1",
			Type:      "TWEARTH-DISASTER",
			Severity:  "critical",
			Title:     "気象特別警報・警報・注意報 (宇都宮地方気象台)",
			Detail:    "【栃木県気象警報・注意報】栃木県では、空気の乾燥した状態が続くため、火の取り扱いに注意してください。",
			Latitude:  36.555,
			Longitude: 139.883,
			Timestamp: now,
		},
		{
			ID:        "dis-jma_extra-cancel-1",
			Type:      "TWEARTH-DISASTER",
			Severity:  "critical",
			Title:     "気象特別警報・警報・注意報 (名瀬測候所)",
			Detail:    "【奄美地方（鹿児島県）気象警報・注意報】注意報を解除します。",
			Latitude:  31.596,
			Longitude: 130.557,
			Timestamp: now.Add(time.Minute),
		},
		{
			ID:        "usgs-far-1",
			Type:      "TWEARTH-QUAKE",
			Severity:  "critical",
			Title:     "M 6.3 - South America",
			Detail:    "Chile",
			TitleKey:  "event.quake.title",
			DetailKey: "event.quake.detail",
			Params: map[string]string{
				"hypocenter": "Chile",
				"magnitude":  "6.3",
				"maxScale":   "-",
			},
			Latitude:  -33.4,
			Longitude: -70.6,
			Timestamp: now.Add(3 * time.Minute),
		},
	} {
		if err := st.SaveEvent(ev); err != nil {
			t.Fatalf("SaveEvent %s: %v", ev.ID, err)
		}
	}

	mgr := NewManager(st)
	disasters := mgr.GetLatestState().Disasters
	if len(disasters) != 2 {
		t.Fatalf("expected 2 disasters after migration (cancellation deleted), got %d: %+v", len(disasters), disasters)
	}

	disMap := make(map[string]store.DisasterRecord)
	for _, d := range disasters {
		disMap[d.ID] = d
	}
	if d := disMap["adv-1"]; d.Severity != "info" || d.AlertLevel != "注意報" {
		t.Fatalf("expected adv-1 reclassified to info/注意報, got %+v", d)
	}
	if d := disMap["warn-naze"]; d.Severity != "warning" || d.AlertLevel != "警報" || d.Latitude > 30.0 {
		t.Fatalf("expected warn-naze reclassified to warning and Amami coords, got %+v", d)
	}

	events, err := st.ListEvents(20)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].ID != "usgs-far-1" {
		t.Fatalf("expected only usgs-far-1 in events after removing info/cancel jma_extra, got %+v", events)
	}
	if events[0].BaseSeverity != "critical" || events[0].Severity != "info" {
		t.Fatalf("expected usgs-far-1 BaseSeverity=critical, Severity=info, got %+v", events[0])
	}

	// Verify idempotency on second NewManager
	_ = NewManager(st)
	if v, ok, _ := st.GetMetaValue(alertMigrationKeyV2); !ok || v != "1" {
		t.Fatalf("expected migration flag set to 1, got ok=%v v=%q", ok, v)
	}
}
