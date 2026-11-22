package collector

import (
	"strconv"
	"strings"

	"github.com/twsnmp/twearth/internal/store"
)

const alertMigrationKeyV2 = "alert_classification_v2"

// migrateAlertsIfNeeded repairs legacy misclassified JMA extra entries (e.g. advisories and
// cancellations tagged as critical) and backfills BaseSeverity / distance-adjusted Severity on
// stored events once per database.
func (m *Manager) migrateAlertsIfNeeded() {
	if m == nil || m.store == nil {
		return
	}
	if val, ok, err := m.store.GetMetaValue(alertMigrationKeyV2); err == nil && ok && val == "1" {
		return
	}

	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}

	_ = m.store.RewriteDisasters("jma_extra", func(rec store.DisasterRecord) (store.DisasterRecord, bool, bool) {
		cat, sev, alertLvl, keep := classifyJmaExtraEntry(rec.Title, rec.Detail)
		if !keep {
			return rec, false, false
		}
		lat, lon := lookupJmaOfficeCoord(rec.AreaName, rec.Title+" "+rec.Detail)
		changed := rec.Category != cat ||
			rec.Severity != sev ||
			rec.AlertLevel != alertLvl ||
			rec.Latitude != lat ||
			rec.Longitude != lon
		if changed {
			rec.Category = cat
			rec.Severity = sev
			rec.AlertLevel = alertLvl
			rec.Latitude = lat
			rec.Longitude = lon
		}
		return rec, changed, true
	})

	_ = m.store.RewriteEvents(func(ev store.EventRecord) (store.EventRecord, bool, bool) {
		switch {
		case strings.HasPrefix(ev.ID, "dis-jma_extra-"):
			_, baseSev, _, keep := classifyJmaExtraEntry(ev.Title, ev.Detail)
			if !keep {
				return ev, false, false
			}
			lat, lon := lookupJmaOfficeCoord("", ev.Title+" "+ev.Detail)
			effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
				Source:    "jma_extra",
				Category:  "disaster",
				Latitude:  lat,
				Longitude: lon,
			}, cfg)
			if effSev == "info" {
				return ev, false, false
			}
			changed := ev.BaseSeverity != baseSev || ev.Severity != effSev || ev.Latitude != lat || ev.Longitude != lon
			ev.BaseSeverity = baseSev
			ev.Severity = effSev
			ev.Latitude = lat
			ev.Longitude = lon
			return ev, changed, true

		case strings.HasPrefix(ev.ID, "dis-mlit_river-"):
			baseSev := ev.BaseSeverity
			if baseSev == "" {
				baseSev = normalizeSeverity(ev.Severity)
			}
			effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
				Source:    "mlit_river",
				Category:  "disaster",
				Latitude:  ev.Latitude,
				Longitude: ev.Longitude,
			}, cfg)
			if effSev == "info" {
				return ev, false, false
			}
			changed := ev.BaseSeverity != baseSev || ev.Severity != effSev
			ev.BaseSeverity = baseSev
			ev.Severity = effSev
			return ev, changed, true

		case strings.Contains(strings.ToUpper(ev.Type), "QUAKE"):
			q := quakeFromEventRecord(ev)
			baseSev := classifyQuakeSeverity(q, cfg.QuakeMinIntensity)
			effSev, _, _ := AdjustQuakeSeverityByDistance(baseSev, q, cfg)
			changed := ev.BaseSeverity != baseSev || ev.Severity != effSev
			ev.BaseSeverity = baseSev
			ev.Severity = effSev
			return ev, changed, true

		default:
			baseSev := ev.BaseSeverity
			if baseSev == "" {
				baseSev = normalizeSeverity(ev.Severity)
			}
			cat := eventTypeToCategory(ev.Type)
			effSev, _, _ := AdjustSeverityByDistance(baseSev, AlertSeverityMeta{
				Category:  cat,
				Latitude:  ev.Latitude,
				Longitude: ev.Longitude,
			}, cfg)
			changed := ev.BaseSeverity != baseSev || ev.Severity != effSev
			ev.BaseSeverity = baseSev
			ev.Severity = effSev
			return ev, changed, true
		}
	})

	_ = m.store.SetMetaValue(alertMigrationKeyV2, "1")
}

func quakeFromEventRecord(ev store.EventRecord) QuakeEvent {
	p := ev.Params
	q := QuakeEvent{
		ID:         ev.ID,
		Title:      ev.Title,
		Hypocenter: ev.Detail,
		Latitude:   ev.Latitude,
		Longitude:  ev.Longitude,
		OccurredAt: ev.Timestamp,
	}
	if p != nil {
		q.Source = p["source"]
		q.MaxScale = p["maxScale"]
		q.Tsunami = p["tsunami"]
		if mag, err := strconv.ParseFloat(p["magnitude"], 64); err == nil {
			q.Magnitude = mag
		}
		if code, err := strconv.Atoi(p["code"]); err == nil {
			q.Code = code
		}
	}
	if q.Code == 0 {
		switch ev.TitleKey {
		case "event.tsunami.title", "event.tsunamiMajor.title", "event.tsunamiWarning.title", "event.tsunamiWatch.title":
			q.Code = 552
		case "event.eew.title":
			q.Code = 556
		default:
			q.Code = 551
		}
	}
	if q.Source == "" {
		if q.MaxScale != "" && q.MaxScale != "-" {
			q.Source = "p2pquake"
		} else if strings.HasPrefix(ev.ID, "usgs-") {
			q.Source = "usgs"
		} else {
			q.Source = "p2pquake"
		}
	}
	return q
}

func eventTypeToCategory(evType string) string {
	up := strings.ToUpper(evType)
	switch {
	case strings.Contains(up, "SPACE"):
		return "space"
	case strings.Contains(up, "LIGHTNING"):
		return "lightning"
	case strings.Contains(up, "PRESSURE"):
		return "pressure"
	case strings.Contains(up, "PRECIP"):
		return "precip"
	case strings.Contains(up, "VOLCANO"):
		return "volcano"
	case strings.Contains(up, "DISASTER"):
		return "disaster"
	default:
		return "other"
	}
}
