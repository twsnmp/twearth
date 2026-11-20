package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"time"

	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
)

const (
	historyChunkDays       = 30
	historyDailyChunkDays  = 365
	historyMarineChunkDays = 90
	historyArchiveLagDays  = 5
	historyChunkDelay      = 2500 * time.Millisecond
)

// HistorySyncStatus represents the live background sync state of a location or dataset.
type HistorySyncStatus struct {
	Kind         string    `json:"kind"` // "weather" | "marine" | "quake"
	LocationID   string    `json:"locationId"`
	LocationName string    `json:"locationName"`
	State        string    `json:"state"` // "idle" | "syncing" | "rate_limited" | "done" | "error"
	EarliestDate string    `json:"earliestDate"`
	LatestDate   string    `json:"latestDate"`
	TargetDate   string    `json:"targetDate"`
	StoredCount  int       `json:"storedCount"`
	ProgressPct  float64   `json:"progressPct"`
	LastError    string    `json:"lastError,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ClearHistoryData deletes stored historical data for scope ("all" | "weather" | "marine" | "quake")
// and resets in-memory sync statuses. If resync is true, it triggers a fresh background sync.
func (m *Manager) ClearHistoryData(scope string, resync bool) error {
	if scope == "" {
		scope = "all"
	}
	if err := m.store.ClearHistoryData(scope); err != nil {
		return err
	}

	m.syncStatusMu.Lock()
	if scope == "all" {
		m.syncStatuses = make(map[string]HistorySyncStatus)
	} else {
		for k, st := range m.syncStatuses {
			if st.Kind == scope {
				delete(m.syncStatuses, k)
			}
		}
	}
	m.syncStatusMu.Unlock()

	if scope == "all" || scope == "quake" {
		m.mu.Lock()
		m.lastQuakes = nil
		m.mu.Unlock()
		m.emit("twearth:quake-history", []QuakeEvent{})
	}

	m.emit("twearth:history-sync-status", m.GetHistorySyncStatus())
	if resync {
		m.TriggerHistorySync()
	}
	return nil
}

// TriggerHistorySync wakes the background history sync worker (canceling any in-flight cycle so new settings take effect immediately).
func (m *Manager) TriggerHistorySync() {
	if platform.GetInfo().IsMobile {
		return
	}
	m.historyCancelMu.Lock()
	if m.historyCancel != nil {
		m.historyCancel()
	}
	m.historyCancelMu.Unlock()

	if m.historySyncCh == nil {
		return
	}
	select {
	case m.historySyncCh <- struct{}{}:
	default:
	}
}

// GetHistorySyncStatus returns a snapshot of all background history sync statuses.
func (m *Manager) GetHistorySyncStatus() []HistorySyncStatus {
	m.syncStatusMu.RLock()
	defer m.syncStatusMu.RUnlock()

	out := make([]HistorySyncStatus, 0, len(m.syncStatuses))
	for _, st := range m.syncStatuses {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].LocationID < out[j].LocationID
	})
	return out
}

func (m *Manager) setSyncStatus(st HistorySyncStatus) {
	st.UpdatedAt = time.Now().UTC()
	key := st.Kind + ":" + st.LocationID
	m.syncStatusMu.Lock()
	if m.syncStatuses == nil {
		m.syncStatuses = make(map[string]HistorySyncStatus)
	}
	m.syncStatuses[key] = st
	m.syncStatusMu.Unlock()
	m.emit("twearth:history-sync-status", m.GetHistorySyncStatus())
}

func (m *Manager) runSyncCycle(parentCtx context.Context) {
	cycleCtx, cancel := context.WithCancel(parentCtx)
	m.historyCancelMu.Lock()
	m.historyCancel = cancel
	m.historyCancelMu.Unlock()
	defer func() {
		cancel()
		m.historyCancelMu.Lock()
		m.historyCancel = nil
		m.historyCancelMu.Unlock()
	}()
	m.syncAllHistory(cycleCtx)
}

func (m *Manager) runHistorySync(ctx context.Context) {
	if platform.GetInfo().IsMobile {
		return
	}
	// Brief startup delay so live polling completes first.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}

	m.runSyncCycle(ctx)

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.historySyncCh:
			m.runSyncCycle(ctx)
		case <-ticker.C:
			m.runSyncCycle(ctx)
		}
	}
}

func (m *Manager) syncAllHistory(ctx context.Context) {
	if platform.GetInfo().IsMobile {
		return
	}
	cfg, err := m.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}
	if cfg.DisableHistorySync {
		return
	}

	now := time.Now().UTC()
	hourlyTarget := now.AddDate(0, 0, -cfg.HourlyHistoryDays).Truncate(24 * time.Hour)
	var dailyTarget time.Time
	if cfg.HistoryDays < 0 {
		// "All": ERA5 weather archive begins 1940-01-01
		dailyTarget = time.Date(1940, 1, 1, 0, 0, 0, 0, time.UTC)
	} else {
		dailyTarget = now.AddDate(0, 0, -cfg.HistoryDays).Truncate(24 * time.Hour)
	}

	targetStr := dailyTarget.Format("2006-01-02")
	for _, loc := range cfg.Locations {
		st, has, _ := m.store.GetHistorySyncState("weather_daily", loc.ID)
		done := has && st.BackfillDone && st.TargetStartDate <= targetStr
		m.refreshDailySyncStatus("weather", loc.ID, loc.Name, targetStr, done, "")
	}
	for _, loc := range cfg.MarineLocations {
		st, has, _ := m.store.GetHistorySyncState("marine_daily", loc.ID)
		done := has && st.BackfillDone && st.TargetStartDate <= targetStr
		m.refreshDailySyncStatus("marine", loc.ID, loc.Name, targetStr, done, "")
	}
	quakeTarget := dailyTarget
	if cfg.HistoryDays < 0 {
		quakeTarget = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	quakeTargetStr := quakeTarget.Format("2006-01-02")
	qSt, qHas, _ := m.store.GetHistorySyncState("quake", "global")
	m.refreshQuakeSyncStatus(quakeTargetStr, qHas && qSt.BackfillDone && qSt.TargetStartDate <= quakeTargetStr, "")

	// Step A: Best-effort catch-up for space weather, LIDEN lightning, and volcano alert levels.
	m.catchUpAuxSources(ctx, cfg)

	// Step B: High-priority catch-up (gap-fill from latest saved timestamp to now, or initial 14d).
	for _, loc := range cfg.Locations {
		if ctx.Err() != nil {
			return
		}
		m.catchUpWeatherLocation(ctx, loc, now)
	}
	for _, loc := range cfg.MarineLocations {
		if ctx.Err() != nil {
			return
		}
		m.catchUpMarineLocation(ctx, loc, now)
	}
	if ctx.Err() == nil {
		m.catchUpQuakes(ctx, now)
	}

	// Step C: Short-term hourly backfill (capped at HourlyHistoryDays, e.g. 90d for mobile efficiency).
	for _, loc := range cfg.Locations {
		if ctx.Err() != nil {
			return
		}
		m.backfillWeatherLocation(ctx, loc, hourlyTarget, now)
	}
	for _, loc := range cfg.MarineLocations {
		if ctx.Err() != nil {
			return
		}
		m.backfillMarineLocation(ctx, loc, hourlyTarget, now)
	}

	// Step D: Long-term daily climate backfill (1y .. All).
	for _, loc := range cfg.Locations {
		if ctx.Err() != nil {
			return
		}
		m.backfillWeatherDailyLocation(ctx, loc, dailyTarget, now)
	}
	for _, loc := range cfg.MarineLocations {
		if ctx.Err() != nil {
			return
		}
		m.backfillMarineDailyLocation(ctx, loc, dailyTarget, now)
	}
	if ctx.Err() == nil {
		quakeTarget := dailyTarget
		if cfg.HistoryDays < 0 {
			quakeTarget = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		m.backfillQuakes(ctx, quakeTarget, now)
	}
}

func (m *Manager) catchUpAuxSources(ctx context.Context, cfg store.Config) {
	primary := cfg.PrimaryLocation()

	// Space weather 7-day Kp catch-up
	if snaps, err := m.space.FetchSpaceHistory(ctx, m.limiter); err == nil && len(snaps) > 0 {
		var pts []store.TimeSeriesPoint
		for _, sp := range snaps {
			if sp.KpIndex >= cfg.KpIndexThreshold {
				m.recordEvent(store.EventRecord{
					ID:        fmt.Sprintf("space-%d", sp.Timestamp.Unix()/900),
					Type:      "TWEARTH-SPACE",
					Severity:  classifySpaceSeverity(sp.KpIndex, ""),
					Title:     fmt.Sprintf("宇宙天気アラート Kp=%.1f", sp.KpIndex),
					Detail:    "NOAA SWPC",
					TitleKey:  "event.space.title",
					DetailKey: "event.space.detail",
					Params: map[string]string{
						"kp":    fmt.Sprintf("%.1f", sp.KpIndex),
						"flare": "-",
					},
					Timestamp: sp.Timestamp,
				})
			}
			pts = append(pts, store.TimeSeriesPoint{
				LocationID: primary.ID,
				Timestamp:  sp.Timestamp.Format("200601021504"),
				KpIndex:    sp.KpIndex,
			})
		}
		// Only save Kp points where no weather snapshot exists yet to avoid overwriting weather fields.
		var missing []store.TimeSeriesPoint
		for _, pt := range pts {
			existing, err := m.store.ListTimeSeries(primary.ID+":"+pt.Timestamp, primary.ID+":"+pt.Timestamp)
			if err == nil && len(existing) == 0 {
				missing = append(missing, pt)
			}
		}
		if len(missing) > 0 {
			_ = m.store.SaveTimeSeriesBatch(missing)
		}
	}

	// LIDEN recent slots catch-up (last ~1-3 hours available in targetTimes_N3.json)
	lastValid, _, _ := m.store.GetMetaValue("liden_last_valid")
	if slots, err := m.liden.FetchLidenRecentSlots(ctx, m.limiter, primary.Latitude, primary.Longitude, cfg.LightningRadiusKm, lastValid, 6); err == nil && len(slots) > 0 {
		newestValid := lastValid
		for _, lid := range slots {
			if lid.ValidTime > newestValid {
				newestValid = lid.ValidTime
			}
			if lid.NearbyCount > 0 {
				ts := time.Now().UTC()
				if parsed, err := time.Parse("20060102150405", lid.ValidTime); err == nil {
					ts = parsed.UTC()
				}
				m.recordEvent(store.EventRecord{
					ID:         "liden-" + lid.ValidTime,
					Type:       "TWEARTH-LIGHTNING",
					Severity:   classifyLightningSeverity(lid.NearbyCount),
					LocationID: primary.ID,
					Title:      fmt.Sprintf("%s %.0fkm 圏内で落雷検知 (%d件)", primary.Name, cfg.LightningRadiusKm, lid.NearbyCount),
					Detail:     fmt.Sprintf("観測時刻: %s", lid.ValidTime),
					TitleKey:   "event.lightning.title",
					DetailKey:  "event.lightning.detail",
					Params: map[string]string{
						"location": primary.Name,
						"radius":   fmt.Sprintf("%.0f", cfg.LightningRadiusKm),
						"count":    fmt.Sprintf("%d", lid.NearbyCount),
						"time":     lid.ValidTime,
					},
					Latitude:  primary.Latitude,
					Longitude: primary.Longitude,
					Timestamp: ts,
				})
			}
		}
		if newestValid != lastValid {
			_ = m.store.SetMetaValue("liden_last_valid", newestValid)
		}
	}

	// Volcano alert level change detection
	if err := m.limiter.Wait(ctx, ProviderJMA); err == nil {
		if vols, err := m.volcano.FetchVolcanoes(ctx); err == nil && len(vols) > 0 {
			prevMap := make(map[string]int)
			if rawPrev, ok, _ := m.store.GetMetaValue("volcano_levels"); ok && rawPrev != "" {
				_ = json.Unmarshal([]byte(rawPrev), &prevMap)
			}
			nextMap := make(map[string]int, len(vols))
			for _, v := range vols {
				nextMap[v.Code] = v.AlertLevel
				if prevLvl, hadPrev := prevMap[v.Code]; hadPrev && v.AlertLevel > prevLvl && v.AlertLevel >= 2 {
					m.recordEvent(store.EventRecord{
						ID:        fmt.Sprintf("volcano-%s-%d-%s", v.Code, v.AlertLevel, time.Now().UTC().Format("20060102")),
						Type:      "TWEARTH-VOLCANO",
						Severity:  "warning",
						Title:     fmt.Sprintf("%s: 噴火警戒レベル %d (%s)", v.Name, v.AlertLevel, v.WarningTitle),
						Detail:    v.WarningTitle,
						Latitude:  v.Latitude,
						Longitude: v.Longitude,
						Timestamp: time.Now().UTC(),
					})
				}
			}
			if encoded, err := json.Marshal(nextMap); err == nil {
				_ = m.store.SetMetaValue("volcano_levels", string(encoded))
			}
		}
	}
}

func (m *Manager) catchUpWeatherLocation(ctx context.Context, loc store.ObservationLocation, now time.Time) {
	earliestTS, latestTS, count, err := m.store.GetTimeSeriesBounds("weather", loc.ID)
	if err != nil {
		return
	}

	var gapStart time.Time
	if count == 0 || latestTS == "" {
		gapStart = now.AddDate(0, 0, -14).Truncate(24 * time.Hour)
	} else {
		latestTime, parseErr := time.Parse("200601021504", latestTS)
		if parseErr != nil || now.Sub(latestTime) < 2*time.Hour {
			return
		}
		gapStart = latestTime.Truncate(24 * time.Hour)
	}

	m.setSyncStatus(HistorySyncStatus{
		Kind:         "weather",
		LocationID:   loc.ID,
		LocationName: loc.Name,
		State:        "syncing",
		EarliestDate: tsToDate(earliestTS),
		LatestDate:   tsToDate(latestTS),
	})

	if err := m.fetchWeatherChunksForward(ctx, loc, gapStart, now); err != nil {
		m.handleSyncError(ctx, "weather", loc.ID, loc.Name, err)
		return
	}
	m.refreshSyncStatusFromBounds("weather", loc.ID, loc.Name, false, "")
}

func (m *Manager) backfillWeatherLocation(ctx context.Context, loc store.ObservationLocation, targetStart, now time.Time) {
	syncState, hasState, _ := m.store.GetHistorySyncState("weather", loc.ID)
	targetStr := targetStart.Format("2006-01-02")
	if hasState && syncState.BackfillDone && syncState.TargetStartDate <= targetStr {
		m.refreshSyncStatusFromBounds("weather", loc.ID, loc.Name, false, "")
		return
	}

	earliestTS, _, count, err := m.store.GetTimeSeriesBounds("weather", loc.ID)
	if err != nil {
		return
	}

	cursorEnd := now.Truncate(24 * time.Hour)
	if count > 0 && earliestTS != "" {
		if t, err := time.Parse("200601021504", earliestTS); err == nil {
			cursorEnd = t.Truncate(24 * time.Hour).AddDate(0, 0, -1)
		}
	}

	if cursorEnd.Before(targetStart) {
		_ = m.store.SaveHistorySyncState(store.HistorySyncState{
			Kind:            "weather",
			LocationID:      loc.ID,
			EarliestTS:      earliestTS,
			TargetStartDate: targetStr,
			BackfillDone:    true,
		})
		m.refreshSyncStatusFromBounds("weather", loc.ID, loc.Name, false, "")
		return
	}

	archiveCutoff := now.AddDate(0, 0, -historyArchiveLagDays).Truncate(24 * time.Hour)
	for !cursorEnd.Before(targetStart) {
		if ctx.Err() != nil {
			return
		}
		chunkStart := cursorEnd.AddDate(0, 0, -(historyChunkDays - 1))
		if chunkStart.Before(targetStart) {
			chunkStart = targetStart
		}

		m.refreshSyncStatusFromBounds("weather", loc.ID, loc.Name, false, "")

		useArchive := !chunkStart.After(archiveCutoff)
		chunkEnd := cursorEnd
		if useArchive && chunkEnd.After(archiveCutoff) {
			chunkEnd = archiveCutoff
		}

		pts, fetchErr := m.openMeteo.FetchWeatherHistoryRange(ctx, loc, chunkStart, chunkEnd, useArchive)
		if fetchErr != nil {
			m.handleSyncError(ctx, "weather", loc.ID, loc.Name, fetchErr)
			return
		}
		if err := m.store.SaveTimeSeriesBatch(pts); err != nil {
			log.Printf("history save weather batch error (%s): %v", loc.ID, err)
			return
		}

		cursorEnd = chunkStart.AddDate(0, 0, -1)
		if !sleepWithJitter(ctx, historyChunkDelay) {
			return
		}
	}

	eTS, lTS, _, _ := m.store.GetTimeSeriesBounds("weather", loc.ID)
	_ = m.store.SaveHistorySyncState(store.HistorySyncState{
		Kind:            "weather",
		LocationID:      loc.ID,
		EarliestTS:      eTS,
		LatestTS:        lTS,
		TargetStartDate: targetStr,
		BackfillDone:    true,
	})
	m.refreshSyncStatusFromBounds("weather", loc.ID, loc.Name, false, "")
}

func (m *Manager) fetchWeatherChunksForward(ctx context.Context, loc store.ObservationLocation, from, to time.Time) error {
	archiveCutoff := to.AddDate(0, 0, -historyArchiveLagDays).Truncate(24 * time.Hour)
	cur := from.Truncate(24 * time.Hour)
	end := to.Truncate(24 * time.Hour)

	for !cur.After(end) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		chunkEnd := cur.AddDate(0, 0, historyChunkDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		useArchive := !cur.After(archiveCutoff)
		if useArchive && chunkEnd.After(archiveCutoff) {
			chunkEnd = archiveCutoff
		}

		pts, err := m.openMeteo.FetchWeatherHistoryRange(ctx, loc, cur, chunkEnd, useArchive)
		if err != nil {
			return err
		}
		if err := m.store.SaveTimeSeriesBatch(pts); err != nil {
			return err
		}
		cur = chunkEnd.AddDate(0, 0, 1)
		if !cur.After(end) && !sleepWithJitter(ctx, historyChunkDelay) {
			return ctx.Err()
		}
	}
	return nil
}

func (m *Manager) catchUpMarineLocation(ctx context.Context, loc store.MarineLocation, now time.Time) {
	earliestTS, latestTS, count, err := m.store.GetTimeSeriesBounds("marine", loc.ID)
	if err != nil {
		return
	}

	var gapStart time.Time
	if count == 0 || latestTS == "" {
		gapStart = now.AddDate(0, 0, -14).Truncate(24 * time.Hour)
	} else {
		latestTime, parseErr := time.Parse("200601021504", latestTS)
		if parseErr != nil || now.Sub(latestTime) < 2*time.Hour {
			return
		}
		gapStart = latestTime.Truncate(24 * time.Hour)
	}

	m.setSyncStatus(HistorySyncStatus{
		Kind:         "marine",
		LocationID:   loc.ID,
		LocationName: loc.Name,
		State:        "syncing",
		EarliestDate: tsToDate(earliestTS),
		LatestDate:   tsToDate(latestTS),
	})

	cur := gapStart.Truncate(24 * time.Hour)
	end := now.Truncate(24 * time.Hour)
	for !cur.After(end) {
		if ctx.Err() != nil {
			return
		}
		chunkEnd := cur.AddDate(0, 0, historyChunkDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		pts, fetchErr := m.openMeteo.FetchMarineHistoryRange(ctx, loc, cur, chunkEnd)
		if fetchErr != nil {
			m.handleSyncError(ctx, "marine", loc.ID, loc.Name, fetchErr)
			return
		}
		if err := m.store.SaveMarineTimeSeriesBatch(pts); err != nil {
			log.Printf("history save marine batch error (%s): %v", loc.ID, err)
			return
		}
		cur = chunkEnd.AddDate(0, 0, 1)
		if !cur.After(end) && !sleepWithJitter(ctx, historyChunkDelay) {
			return
		}
	}
	m.refreshSyncStatusFromBounds("marine", loc.ID, loc.Name, false, "")
}

func (m *Manager) backfillMarineLocation(ctx context.Context, loc store.MarineLocation, targetStart, now time.Time) {
	syncState, hasState, _ := m.store.GetHistorySyncState("marine", loc.ID)
	targetStr := targetStart.Format("2006-01-02")
	if hasState && syncState.BackfillDone && syncState.TargetStartDate <= targetStr {
		m.refreshSyncStatusFromBounds("marine", loc.ID, loc.Name, false, "")
		return
	}

	earliestTS, _, count, err := m.store.GetTimeSeriesBounds("marine", loc.ID)
	if err != nil {
		return
	}

	cursorEnd := now.Truncate(24 * time.Hour)
	if count > 0 && earliestTS != "" {
		if t, err := time.Parse("200601021504", earliestTS); err == nil {
			cursorEnd = t.Truncate(24 * time.Hour).AddDate(0, 0, -1)
		}
	}

	for !cursorEnd.Before(targetStart) {
		if ctx.Err() != nil {
			return
		}
		chunkStart := cursorEnd.AddDate(0, 0, -(historyChunkDays - 1))
		if chunkStart.Before(targetStart) {
			chunkStart = targetStart
		}

		m.refreshSyncStatusFromBounds("marine", loc.ID, loc.Name, false, "")

		pts, fetchErr := m.openMeteo.FetchMarineHistoryRange(ctx, loc, chunkStart, cursorEnd)
		if fetchErr != nil {
			var statusErr *HTTPStatusError
			if errors.As(fetchErr, &statusErr) && statusErr.StatusCode == http.StatusBadRequest {
				// Reached earliest available date on Marine API
				break
			}
			m.handleSyncError(ctx, "marine", loc.ID, loc.Name, fetchErr)
			return
		}
		if len(pts) == 0 {
			// No more historical marine data available
			break
		}
		if err := m.store.SaveMarineTimeSeriesBatch(pts); err != nil {
			log.Printf("history save marine batch error (%s): %v", loc.ID, err)
			return
		}

		cursorEnd = chunkStart.AddDate(0, 0, -1)
		if !sleepWithJitter(ctx, historyChunkDelay) {
			return
		}
	}

	eTS, lTS, _, _ := m.store.GetTimeSeriesBounds("marine", loc.ID)
	_ = m.store.SaveHistorySyncState(store.HistorySyncState{
		Kind:            "marine",
		LocationID:      loc.ID,
		EarliestTS:      eTS,
		LatestTS:        lTS,
		TargetStartDate: targetStr,
		BackfillDone:    true,
	})
	m.refreshSyncStatusFromBounds("marine", loc.ID, loc.Name, false, "")
}

func (m *Manager) backfillWeatherDailyLocation(ctx context.Context, loc store.ObservationLocation, targetStart, now time.Time) {
	syncState, hasState, _ := m.store.GetHistorySyncState("weather_daily", loc.ID)
	targetStr := targetStart.Format("2006-01-02")
	if hasState && syncState.BackfillDone && syncState.TargetStartDate <= targetStr {
		m.refreshDailySyncStatus("weather", loc.ID, loc.Name, targetStr, true, "")
		return
	}

	earliestDate, latestDate, count, err := m.store.GetDailyBounds("weather", loc.ID)
	if err != nil {
		return
	}

	// First catch up daily forward to today if we already have daily data
	archiveCutoff := now.AddDate(0, 0, -historyArchiveLagDays).Truncate(24 * time.Hour)
	if count > 0 && latestDate != "" {
		if lt, err := time.Parse("20060102", latestDate); err == nil && now.Sub(lt) > 36*time.Hour {
			cur := lt.AddDate(0, 0, 1)
			end := now.Truncate(24 * time.Hour)
			if !cur.After(end) {
				useArch := !cur.After(archiveCutoff)
				cEnd := end
				if useArch && cEnd.After(archiveCutoff) {
					cEnd = archiveCutoff
				}
				if pts, err := m.openMeteo.FetchWeatherDailyRange(ctx, loc, cur, cEnd, useArch); err == nil {
					_ = m.store.SaveDailyWeatherBatch(pts)
				}
			}
		}
	}

	cursorEnd := now.Truncate(24 * time.Hour)
	if count > 0 && earliestDate != "" {
		if t, err := time.Parse("20060102", earliestDate); err == nil {
			cursorEnd = t.AddDate(0, 0, -1)
		}
	}

	for !cursorEnd.Before(targetStart) {
		if ctx.Err() != nil {
			return
		}
		chunkStart := cursorEnd.AddDate(0, 0, -(historyDailyChunkDays - 1))
		if chunkStart.Before(targetStart) {
			chunkStart = targetStart
		}

		m.refreshDailySyncStatus("weather", loc.ID, loc.Name, targetStr, false, "")

		useArchive := !chunkStart.After(archiveCutoff)
		chunkEnd := cursorEnd
		if useArchive && chunkEnd.After(archiveCutoff) {
			chunkEnd = archiveCutoff
		}

		pts, fetchErr := m.openMeteo.FetchWeatherDailyRange(ctx, loc, chunkStart, chunkEnd, useArchive)
		if fetchErr != nil {
			var statusErr *HTTPStatusError
			if errors.As(fetchErr, &statusErr) && statusErr.StatusCode == http.StatusBadRequest {
				break
			}
			m.handleSyncError(ctx, "weather", loc.ID, loc.Name, fetchErr)
			return
		}
		if len(pts) == 0 && useArchive {
			break
		}
		if err := m.store.SaveDailyWeatherBatch(pts); err != nil {
			log.Printf("history save daily weather error (%s): %v", loc.ID, err)
			return
		}

		cursorEnd = chunkStart.AddDate(0, 0, -1)
		if !sleepWithJitter(ctx, historyChunkDelay) {
			return
		}
	}

	eD, lD, _, _ := m.store.GetDailyBounds("weather", loc.ID)
	_ = m.store.SaveHistorySyncState(store.HistorySyncState{
		Kind:            "weather_daily",
		LocationID:      loc.ID,
		EarliestTS:      eD,
		LatestTS:        lD,
		TargetStartDate: targetStr,
		BackfillDone:    true,
	})
	m.refreshDailySyncStatus("weather", loc.ID, loc.Name, targetStr, true, "")
}

func (m *Manager) backfillMarineDailyLocation(ctx context.Context, loc store.MarineLocation, targetStart, now time.Time) {
	syncState, hasState, _ := m.store.GetHistorySyncState("marine_daily", loc.ID)
	targetStr := targetStart.Format("2006-01-02")
	if hasState && syncState.BackfillDone && syncState.TargetStartDate <= targetStr {
		m.refreshDailySyncStatus("marine", loc.ID, loc.Name, targetStr, true, "")
		return
	}

	earliestDate, latestDate, count, err := m.store.GetDailyBounds("marine", loc.ID)
	if err != nil {
		return
	}

	if count > 0 && latestDate != "" {
		if lt, err := time.Parse("20060102", latestDate); err == nil && now.Sub(lt) > 36*time.Hour {
			if pts, err := m.openMeteo.FetchMarineDailyRange(ctx, loc, lt.AddDate(0, 0, 1), now); err == nil {
				_ = m.store.SaveDailyMarineBatch(pts)
			}
		}
	}

	cursorEnd := now.Truncate(24 * time.Hour)
	if count > 0 && earliestDate != "" {
		if t, err := time.Parse("20060102", earliestDate); err == nil {
			cursorEnd = t.AddDate(0, 0, -1)
		}
	}

	for !cursorEnd.Before(targetStart) {
		if ctx.Err() != nil {
			return
		}
		chunkStart := cursorEnd.AddDate(0, 0, -(historyMarineChunkDays - 1))
		if chunkStart.Before(targetStart) {
			chunkStart = targetStart
		}

		m.refreshDailySyncStatus("marine", loc.ID, loc.Name, targetStr, false, "")

		pts, fetchErr := m.openMeteo.FetchMarineDailyRange(ctx, loc, chunkStart, cursorEnd)
		if fetchErr != nil {
			var statusErr *HTTPStatusError
			if errors.As(fetchErr, &statusErr) && statusErr.StatusCode == http.StatusBadRequest {
				break
			}
			m.handleSyncError(ctx, "marine", loc.ID, loc.Name, fetchErr)
			return
		}
		if len(pts) == 0 {
			break
		}
		if err := m.store.SaveDailyMarineBatch(pts); err != nil {
			log.Printf("history save daily marine error (%s): %v", loc.ID, err)
			return
		}

		cursorEnd = chunkStart.AddDate(0, 0, -1)
		if !sleepWithJitter(ctx, historyChunkDelay) {
			return
		}
	}

	eD, lD, _, _ := m.store.GetDailyBounds("marine", loc.ID)
	_ = m.store.SaveHistorySyncState(store.HistorySyncState{
		Kind:            "marine_daily",
		LocationID:      loc.ID,
		EarliestTS:      eD,
		LatestTS:        lD,
		TargetStartDate: targetStr,
		BackfillDone:    true,
	})
	m.refreshDailySyncStatus("marine", loc.ID, loc.Name, targetStr, true, "")
}

func (m *Manager) refreshDailySyncStatus(kind, locID, locName, targetDate string, done bool, lastErr string) {
	eD, lD, dCount, _ := m.store.GetDailyBounds(kind, locID)
	_, _, hCount, _ := m.store.GetTimeSeriesBounds(kind, locID)
	state := "syncing"
	pct := 0.0
	if done {
		state = "done"
		pct = 100.0
	} else if eD != "" && lD != "" && targetDate != "" {
		eTime, err1 := time.Parse("20060102", eD)
		lTime, err2 := time.Parse("20060102", lD)
		tTime, err3 := time.Parse("2006-01-02", targetDate)
		if err1 == nil && err2 == nil && err3 == nil {
			total := lTime.Sub(tTime).Hours()
			if total > 0 {
				pct = (lTime.Sub(eTime).Hours() / total) * 100.0
				if pct > 99 {
					pct = 99
				}
				if pct < 0 {
					pct = 0
				}
			}
		}
	}
	m.setSyncStatus(HistorySyncStatus{
		Kind:         kind,
		LocationID:   locID,
		LocationName: locName,
		State:        state,
		EarliestDate: tsToDate(eD),
		LatestDate:   tsToDate(lD),
		TargetDate:   targetDate,
		StoredCount:  dCount + hCount,
		ProgressPct:  pct,
		LastError:    lastErr,
	})
}

func (m *Manager) catchUpQuakes(ctx context.Context, now time.Time) {
	_, latest, count, err := m.store.GetQuakeBounds()
	if err != nil {
		return
	}

	var gapStart time.Time
	if count == 0 || latest.IsZero() {
		gapStart = now.AddDate(0, 0, -30)
	} else if now.Sub(latest) < time.Hour {
		return
	} else {
		gapStart = latest.Add(-10 * time.Minute)
	}

	m.setSyncStatus(HistorySyncStatus{
		Kind:         "quake",
		LocationID:   "global",
		LocationName: "Earthquakes (USGS/P2P)",
		State:        "syncing",
	})

	if usgs, err := m.quake.FetchUSGSHistoryRange(ctx, m.limiter, gapStart, now, 4.0); err == nil && len(usgs) > 0 {
		_ = m.saveQuakeEventsToStore(usgs)
	}
	if p2p, err := m.quake.FetchP2PHistoryPage(ctx, m.limiter, 50, 0); err == nil && len(p2p) > 0 {
		_ = m.saveQuakeEventsToStore(p2p)
	}
	m.refreshQuakeSyncStatus("", false, "")
}

func (m *Manager) backfillQuakes(ctx context.Context, targetStart, now time.Time) {
	syncState, hasState, _ := m.store.GetHistorySyncState("quake", "global")
	targetStr := targetStart.Format("2006-01-02")
	if hasState && syncState.BackfillDone && syncState.TargetStartDate <= targetStr {
		m.refreshQuakeSyncStatus(targetStr, true, "")
		return
	}

	earliest, _, count, err := m.store.GetQuakeBounds()
	if err != nil {
		return
	}
	cursorEnd := now
	if count > 0 && !earliest.IsZero() {
		cursorEnd = earliest.Add(-time.Second)
	}

	// Fetch up to 5 pages of P2PQuake history for domestic Japan detail
	for page := 1; page < 5; page++ {
		if ctx.Err() != nil {
			return
		}
		p2p, err := m.quake.FetchP2PHistoryPage(ctx, m.limiter, 100, page*100)
		if err != nil || len(p2p) == 0 {
			break
		}
		_ = m.saveQuakeEventsToStore(p2p)
		if !sleepWithJitter(ctx, 2*time.Second) {
			return
		}
	}

	for cursorEnd.After(targetStart) {
		if ctx.Err() != nil {
			return
		}
		chunkStart := cursorEnd.AddDate(0, 0, -historyChunkDays)
		if chunkStart.Before(targetStart) {
			chunkStart = targetStart
		}
		m.refreshQuakeSyncStatus(targetStr, false, "")
		minMag := 4.5
		if now.Sub(chunkStart) > 365*24*time.Hour {
			minMag = 5.0
		}
		var usgs []QuakeEvent
		var fetchErr error
		for retry := 0; retry < 3; retry++ {
			usgs, fetchErr = m.quake.FetchUSGSHistoryRange(ctx, m.limiter, chunkStart, cursorEnd, minMag)
			if fetchErr == nil {
				break
			}
			if ctx.Err() != nil {
				return
			}
			var statusErr *HTTPStatusError
			if errors.As(fetchErr, &statusErr) && (statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode == http.StatusServiceUnavailable) {
				break
			}
			if !sleepWithJitter(ctx, time.Duration(retry+1)*5*time.Second) {
				return
			}
		}
		if fetchErr != nil {
			m.handleSyncError(ctx, "quake", "global", "Earthquakes", fetchErr)
			return
		}
		if len(usgs) > 0 {
			_ = m.saveQuakeEventsToStore(usgs)
		}
		cursorEnd = chunkStart.Add(-time.Second)
		if !sleepWithJitter(ctx, 2*time.Second) {
			return
		}
	}

	e, l, _, _ := m.store.GetQuakeBounds()
	_ = m.store.SaveHistorySyncState(store.HistorySyncState{
		Kind:            "quake",
		LocationID:      "global",
		EarliestTS:      e.UTC().Format("200601021504"),
		LatestTS:        l.UTC().Format("200601021504"),
		TargetStartDate: targetStr,
		BackfillDone:    true,
	})
	m.refreshQuakeSyncStatus(targetStr, true, "")
}

func (m *Manager) saveQuakeEventsToStore(events []QuakeEvent) error {
	if len(events) == 0 {
		return nil
	}
	recs := make([]store.QuakeRecord, 0, len(events))
	for _, ev := range events {
		recs = append(recs, store.QuakeRecord{
			ID:         ev.ID,
			Source:     ev.Source,
			Code:       ev.Code,
			Title:      ev.Title,
			Hypocenter: ev.Hypocenter,
			Magnitude:  ev.Magnitude,
			MaxScale:   ev.MaxScale,
			Latitude:   ev.Latitude,
			Longitude:  ev.Longitude,
			DepthKm:    ev.DepthKm,
			Tsunami:    ev.Tsunami,
			OccurredAt: ev.OccurredAt,
		})
	}
	return m.store.SaveQuakeBatch(recs)
}

func (m *Manager) handleSyncError(ctx context.Context, kind, locID, locName string, err error) {
	m.syncStatusMu.RLock()
	prev := m.syncStatuses[kind+":"+locID]
	m.syncStatusMu.RUnlock()

	provider := ProviderOpenMeteo
	if kind == "quake" {
		provider = ProviderUSGS
	}

	state := "error"
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) && (statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode == http.StatusServiceUnavailable) {
		state = "rate_limited"
		wait := statusErr.RetryAfter
		if wait <= 0 {
			wait = 30 * time.Second
		}
		m.limiter.SetBackoff(provider, wait)
		m.setSyncStatus(HistorySyncStatus{
			Kind:         kind,
			LocationID:   locID,
			LocationName: locName,
			State:        state,
			EarliestDate: prev.EarliestDate,
			LatestDate:   prev.LatestDate,
			TargetDate:   prev.TargetDate,
			StoredCount:  prev.StoredCount,
			ProgressPct:  prev.ProgressPct,
			LastError:    err.Error(),
		})
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		return
	}
	m.setSyncStatus(HistorySyncStatus{
		Kind:         kind,
		LocationID:   locID,
		LocationName: locName,
		State:        state,
		EarliestDate: prev.EarliestDate,
		LatestDate:   prev.LatestDate,
		TargetDate:   prev.TargetDate,
		StoredCount:  prev.StoredCount,
		ProgressPct:  prev.ProgressPct,
		LastError:    err.Error(),
	})
}

func (m *Manager) refreshSyncStatusFromBounds(kind, locID, locName string, done bool, lastErr string) {
	eTS, lTS, hCount, _ := m.store.GetTimeSeriesBounds(kind, locID)
	eD, lD, dCount, _ := m.store.GetDailyBounds(kind, locID)
	earliest := tsToDate(eTS)
	if eD != "" && (earliest == "" || tsToDate(eD) < earliest) {
		earliest = tsToDate(eD)
	}
	latest := tsToDate(lTS)
	if lD != "" && (latest == "" || tsToDate(lD) > latest) {
		latest = tsToDate(lD)
	}
	state := "syncing"
	pct := 0.0
	if done {
		state = "done"
		pct = 100.0
	} else if eTS != "" && lTS != "" {
		cfg, _ := m.store.GetConfig()
		eTime, err1 := time.Parse("200601021504", eTS)
		lTime, err2 := time.Parse("200601021504", lTS)
		if err1 == nil && err2 == nil && cfg.HourlyHistoryDays > 0 {
			days := lTime.Sub(eTime).Hours() / 24.0
			pct = (days / float64(cfg.HourlyHistoryDays)) * 100.0
			if pct > 99 {
				pct = 99
			}
			if pct < 0 {
				pct = 0
			}
		}
	}
	m.setSyncStatus(HistorySyncStatus{
		Kind:         kind,
		LocationID:   locID,
		LocationName: locName,
		State:        state,
		EarliestDate: earliest,
		LatestDate:   latest,
		StoredCount:  hCount + dCount,
		ProgressPct:  pct,
		LastError:    lastErr,
	})
}

func (m *Manager) refreshQuakeSyncStatus(targetDate string, done bool, lastErr string) {
	e, l, count, _ := m.store.GetQuakeBounds()
	state := "syncing"
	pct := 0.0
	if done {
		state = "done"
		pct = 100.0
	} else if !e.IsZero() && !l.IsZero() && targetDate != "" {
		if tTime, err := time.Parse("2006-01-02", targetDate); err == nil {
			total := l.Sub(tTime).Hours()
			if total > 0 {
				pct = (l.Sub(e).Hours() / total) * 100.0
				if pct > 99 {
					pct = 99
				}
				if pct < 0 {
					pct = 0
				}
			}
		}
	}
	eStr, lStr := "", ""
	if !e.IsZero() {
		eStr = e.UTC().Format("2006-01-02")
	}
	if !l.IsZero() {
		lStr = l.UTC().Format("2006-01-02")
	}
	m.setSyncStatus(HistorySyncStatus{
		Kind:         "quake",
		LocationID:   "global",
		LocationName: "Earthquakes",
		State:        state,
		EarliestDate: eStr,
		LatestDate:   lStr,
		TargetDate:   targetDate,
		StoredCount:  count,
		ProgressPct:  pct,
		LastError:    lastErr,
	})
}

func tsToDate(ts string) string {
	if len(ts) < 8 {
		return ""
	}
	return ts[:4] + "-" + ts[4:6] + "-" + ts[6:8]
}

func sleepWithJitter(ctx context.Context, base time.Duration) bool {
	d := base + time.Duration(rand.Intn(800))*time.Millisecond
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
