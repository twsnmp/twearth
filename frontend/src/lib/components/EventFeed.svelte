<script lang="ts">
  import { appState, focusMapAt, setEventWindowHours } from "../stores/appState.svelte";
  import { i18nState, t } from "../i18n/index.svelte";
  import { localizeHypocenter, localizeLegacyEventText, localizeScale } from "../i18n/quakeText";
  import type { EventRecord } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import type { QuakeEvent } from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";

  type Severity = "critical" | "warning" | "info";
  type Category = "quake" | "lightning" | "pressure" | "precip" | "space" | "other";
  type SeverityFilter = "all" | "critical" | "warning_up" | "info";
  type SortOrder = "newest" | "oldest" | "severity_desc" | "severity_asc";

  interface NormalizedFeedItem {
    id: string;
    category: Category;
    typeLabel: string;
    severity: Severity;
    title: string;
    detail: string;
    timestamp: string;
    timeMs: number;
    latitude?: number;
    longitude?: number;
    zoom: number;
    badges: string[];
  }

  const keyMap: Record<string, string> = {
    "event.pressureDrop.title": "events.pressureDropTitle",
    "event.pressureDrop.detail": "events.pressureDropDetail",
    "event.rain.title": "events.rainTitle",
    "event.rain.detail": "events.rainDetail",
    "event.snow.title": "events.snowTitle",
    "event.snow.detail": "events.snowDetail",
    "event.lightning.title": "events.lightningTitle",
    "event.lightning.detail": "events.lightningDetail",
    "event.space.title": "events.spaceTitle",
    "event.space.detail": "events.spaceDetail",
    "event.quake.title": "events.quakeTitle",
    "event.quake.detail": "events.quakeDetail",
    "event.tsunami.title": "events.tsunamiTitle",
    "event.eew.title": "events.eewTitle",
  };

  const severityRank: Record<Severity, number> = {
    critical: 3,
    warning: 2,
    info: 1,
  };

  const windowOptions = [
    { hours: 24, key: "events.window24h" },
    { hours: 72, key: "events.window3d" },
    { hours: 168, key: "events.window7d" },
    { hours: 720, key: "events.window30d" },
    { hours: -1, key: "events.windowAll" },
  ];

  let severityFilter = $state<SeverityFilter>("all");
  let categoryFilter = $state<"all" | Category>("all");
  let sortOrder = $state<SortOrder>("newest");
  let pinCritical = $state(true);
  let searchQuery = $state("");

  function scaleRank(scale?: string): number {
    switch ((scale ?? "").trim()) {
      case "1":
        return 10;
      case "2":
        return 20;
      case "3":
        return 30;
      case "4":
        return 40;
      case "5-":
      case "5弱":
        return 45;
      case "5+":
      case "5強":
        return 50;
      case "6-":
      case "6弱":
        return 55;
      case "6+":
      case "6強":
        return 60;
      case "7":
        return 70;
      default:
        return 0;
    }
  }

  function resolveEventText(
    key: string | undefined,
    fallback: string,
    params?: Record<string, string | undefined> | null
  ): string {
    if (key && keyMap[key]) {
      const normalizedParams = params
        ? {
            ...params,
            hypocenter: params.hypocenter ? localizeHypocenter(params.hypocenter) : undefined,
            maxScale: params.maxScale ? localizeScale(params.maxScale) : undefined,
          }
        : undefined;
      return t(keyMap[key], normalizedParams);
    }
    return localizeLegacyEventText(fallback);
  }

  function detectCategory(typeStr: string): Category {
    const up = typeStr.toUpperCase();
    if (up.includes("QUAKE") || up.includes("TSUNAMI") || up.includes("EEW")) return "quake";
    if (up.includes("LIGHTNING")) return "lightning";
    if (up.includes("PRESSURE")) return "pressure";
    if (up.includes("PRECIP") || up.includes("RAIN") || up.includes("SNOW")) return "precip";
    if (up.includes("SPACE")) return "space";
    return "other";
  }

  function getCategoryLabel(cat: Category, fallbackType: string): string {
    switch (cat) {
      case "quake":
        return t("events.typeQuake");
      case "lightning":
        return t("events.typeLightning");
      case "pressure":
        return t("events.typePressure");
      case "precip":
        return t("events.typePrecip");
      case "space":
        return t("events.typeSpace");
      default:
        return fallbackType;
    }
  }

  function inferEventSeverity(ev: EventRecord, cat: Category): Severity {
    if (ev.severity === "critical") return "critical";
    const params = ev.params ?? {};

    if (cat === "quake") {
      if (ev.titleKey === "event.tsunami.title" || ev.titleKey === "event.eew.title") {
        return "critical";
      }
      const mag = parseFloat(params.magnitude ?? "");
      const rank = scaleRank(params.maxScale);
      if (rank >= 45 || (!Number.isNaN(mag) && mag >= 6.0)) return "critical";
      if (rank >= 30 || (!Number.isNaN(mag) && mag >= 4.5)) return "warning";
    } else if (cat === "pressure") {
      const delta = parseFloat(params.delta ?? "");
      if (!Number.isNaN(delta) && delta <= -8.0) return "critical";
    } else if (cat === "lightning") {
      const cnt = parseInt(params.count ?? "", 10);
      if (!Number.isNaN(cnt) && cnt >= 10) return "critical";
    } else if (cat === "space") {
      const kp = parseFloat(params.kp ?? "");
      if ((!Number.isNaN(kp) && kp >= 7.0) || params.flare === "X") return "critical";
    }

    if (ev.severity === "warning") return "warning";
    return "info";
  }

  function inferQuakeSeverity(q: QuakeEvent): Severity {
    const rank = scaleRank(q.maxScale);
    if (q.code === 552 || q.code === 556 || rank >= 45 || q.magnitude >= 6.0) {
      return "critical";
    }
    if (rank >= 30 || q.magnitude >= 4.5) {
      return "warning";
    }
    return "info";
  }

  function buildEventBadges(ev: EventRecord, cat: Category): string[] {
    const p = ev.params ?? {};
    const badges: string[] = [];
    if (cat === "quake") {
      if (p.maxScale) badges.push(`${i18nState.lang === "ja" ? "震度" : "Int"} ${localizeScale(p.maxScale)}`);
      if (p.magnitude && parseFloat(p.magnitude) > 0) badges.push(`M${p.magnitude}`);
    } else if (cat === "pressure") {
      if (p.delta) badges.push(`${p.delta} hPa/3h`);
    } else if (cat === "lightning") {
      if (p.count) badges.push(`${p.count}${i18nState.lang === "ja" ? "件" : " strikes"}`);
    } else if (cat === "space") {
      if (p.kp) badges.push(`Kp ${p.kp}`);
      if (p.flare) badges.push(`${p.flare}-class`);
    }
    return badges;
  }

  function buildQuakeBadges(q: QuakeEvent): string[] {
    const badges: string[] = [];
    if (q.maxScale) badges.push(`${i18nState.lang === "ja" ? "震度" : "Int"} ${localizeScale(q.maxScale)}`);
    if (q.magnitude > 0) badges.push(`M${q.magnitude.toFixed(1)}`);
    return badges;
  }

  const unifiedItems = $derived.by((): NormalizedFeedItem[] => {
    const byId = new Map<string, NormalizedFeedItem>();
    const cutoffMs =
      appState.eventWindowHours > 0 ? Date.now() - appState.eventWindowHours * 3600 * 1000 : 0;

    for (const ev of appState.events) {
      const ms = new Date(ev.timestamp).getTime();
      if (cutoffMs > 0 && !Number.isNaN(ms) && ms < cutoffMs) continue;
      const cat = detectCategory(ev.type);
      const sev = inferEventSeverity(ev, cat);
      byId.set(ev.id, {
        id: ev.id,
        category: cat,
        typeLabel: getCategoryLabel(cat, ev.type),
        severity: sev,
        title: resolveEventText(ev.titleKey, ev.title, ev.params),
        detail: ev.detail || ev.detailKey ? resolveEventText(ev.detailKey, ev.detail, ev.params) : "",
        timestamp: ev.timestamp,
        timeMs: Number.isNaN(ms) ? 0 : ms,
        latitude: ev.latitude,
        longitude: ev.longitude,
        zoom: cat === "quake" ? 6.5 : 7,
        badges: buildEventBadges(ev, cat),
      });
    }

    for (const q of appState.quakes) {
      const ms = new Date(q.occurredAt).getTime();
      if (cutoffMs > 0 && !Number.isNaN(ms) && ms < cutoffMs) continue;
      if (byId.has(q.id)) continue;
      const sev = inferQuakeSeverity(q);
      const depthLabel = q.depthKm > 0 ? ` (${q.depthKm.toFixed(0)} km)` : "";
      byId.set(q.id, {
        id: q.id,
        category: "quake",
        typeLabel: `${getCategoryLabel("quake", "QUAKE")} · ${q.source.toUpperCase()}`,
        severity: sev,
        title: localizeLegacyEventText(q.title),
        detail: q.hypocenter ? `${localizeHypocenter(q.hypocenter)}${depthLabel}` : "",
        timestamp: q.occurredAt,
        timeMs: Number.isNaN(ms) ? 0 : ms,
        latitude: q.latitude || undefined,
        longitude: q.longitude || undefined,
        zoom: 6.5,
        badges: buildQuakeBadges(q),
      });
    }

    return Array.from(byId.values());
  });

  const severityCounts = $derived.by(() => {
    let critical = 0;
    let warning = 0;
    let info = 0;
    for (const item of unifiedItems) {
      if (item.severity === "critical") critical++;
      else if (item.severity === "warning") warning++;
      else info++;
    }
    return { critical, warning, info, total: unifiedItems.length };
  });

  const filteredItems = $derived.by(() => {
    const q = searchQuery.trim().toLowerCase();
    const list = unifiedItems.filter((item) => {
      if (severityFilter === "critical" && item.severity !== "critical") return false;
      if (severityFilter === "warning_up" && item.severity === "info") return false;
      if (severityFilter === "info" && item.severity !== "info") return false;
      if (categoryFilter !== "all" && item.category !== categoryFilter) return false;
      if (q) {
        const hay = `${item.title} ${item.detail} ${item.typeLabel} ${item.badges.join(" ")}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });

    list.sort((a, b) => {
      if (sortOrder === "newest") return b.timeMs - a.timeMs;
      if (sortOrder === "oldest") return a.timeMs - b.timeMs;
      if (sortOrder === "severity_desc") {
        const diff = severityRank[b.severity] - severityRank[a.severity];
        return diff !== 0 ? diff : b.timeMs - a.timeMs;
      }
      const diff = severityRank[a.severity] - severityRank[b.severity];
      return diff !== 0 ? diff : b.timeMs - a.timeMs;
    });

    return list;
  });

  const shouldSplitPinned = $derived(
    pinCritical &&
      (sortOrder === "newest" || sortOrder === "oldest") &&
      severityFilter !== "info" &&
      filteredItems.some((i) => i.severity === "critical") &&
      filteredItems.some((i) => i.severity !== "critical")
  );

  const pinnedItems = $derived(shouldSplitPinned ? filteredItems.filter((i) => i.severity === "critical") : []);
  const regularItems = $derived(shouldSplitPinned ? filteredItems.filter((i) => i.severity !== "critical") : filteredItems);

  const hasActiveFilters = $derived(
    severityFilter !== "all" || categoryFilter !== "all" || searchQuery.trim() !== ""
  );

  function resetFilters() {
    severityFilter = "all";
    categoryFilter = "all";
    searchQuery = "";
  }

  function toggleSeveritySummary(target: "critical" | "warning_up" | "info") {
    severityFilter = severityFilter === target ? "all" : target;
  }

  function getSeverityLabel(sev: Severity): string {
    if (sev === "critical") return t("events.severityCritical");
    if (sev === "warning") return t("events.severityWarning");
    return t("events.severityInfo");
  }

  function getSeveritySymbol(sev: Severity): string {
    if (sev === "critical") return "⚠";
    if (sev === "warning") return "▲";
    return "ℹ";
  }

  function formatTime(ts: string): string {
    if (!ts) return "";
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    return d.toLocaleString(i18nState.lang === "ja" ? "ja-JP" : "en-US", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  }
</script>

<div class="feed-wrap">
  <div class="feed-header">
    <div class="header-title-row">
      <h3>{t("events.title")}</h3>
      <span class="total-count">{filteredItems.length} / {severityCounts.total}</span>
    </div>
    <div class="summary-pills">
      <button
        type="button"
        class="summary-pill critical"
        class:active={severityFilter === "critical"}
        onclick={() => toggleSeveritySummary("critical")}
      >
        <span>⚠ {t("events.severityCritical")}</span>
        <strong>{severityCounts.critical}</strong>
      </button>
      <button
        type="button"
        class="summary-pill warning"
        class:active={severityFilter === "warning_up"}
        onclick={() => toggleSeveritySummary("warning_up")}
      >
        <span>▲ {t("events.severityWarning")}</span>
        <strong>{severityCounts.warning}</strong>
      </button>
      <button
        type="button"
        class="summary-pill info"
        class:active={severityFilter === "info"}
        onclick={() => toggleSeveritySummary("info")}
      >
        <span>ℹ {t("events.severityInfo")}</span>
        <strong>{severityCounts.info}</strong>
      </button>
    </div>
  </div>

  <div class="feed-controls">
    <div class="window-chips" role="group" aria-label="Event Time Window">
      {#each windowOptions as opt (opt.hours)}
        <button
          type="button"
          class="chip-btn"
          class:active={appState.eventWindowHours === opt.hours}
          disabled={appState.eventsLoading}
          onclick={() => setEventWindowHours(opt.hours)}
        >
          {t(opt.key)}
        </button>
      {/each}
    </div>

    <div class="filter-row">
      <select class="ctrl-select" bind:value={severityFilter} aria-label="Severity Filter">
        <option value="all">{t("events.severityAll")}</option>
        <option value="critical">{t("events.severityCritical")}</option>
        <option value="warning_up">{t("events.severityWarningUp")}</option>
        <option value="info">{t("events.severityInfo")}</option>
      </select>

      <select class="ctrl-select" bind:value={categoryFilter} aria-label="Type Filter">
        <option value="all">{t("events.typeAll")}</option>
        <option value="quake">{t("events.typeQuake")}</option>
        <option value="lightning">{t("events.typeLightning")}</option>
        <option value="pressure">{t("events.typePressure")}</option>
        <option value="precip">{t("events.typePrecip")}</option>
        <option value="space">{t("events.typeSpace")}</option>
      </select>

      <select class="ctrl-select" bind:value={sortOrder} aria-label="Sort Order">
        <option value="newest">{t("events.sortNewest")}</option>
        <option value="oldest">{t("events.sortOldest")}</option>
        <option value="severity_desc">{t("events.sortSeverityDesc")}</option>
        <option value="severity_asc">{t("events.sortSeverityAsc")}</option>
      </select>
    </div>

    <div class="search-row">
      <input
        type="search"
        class="search-input"
        placeholder={t("events.searchPlaceholder")}
        bind:value={searchQuery}
      />
      <label class="pin-toggle">
        <input type="checkbox" bind:checked={pinCritical} />
        <span>{t("events.pinCritical")}</span>
      </label>
      {#if hasActiveFilters}
        <button type="button" class="reset-btn" onclick={resetFilters}>
          {t("events.resetFilters")}
        </button>
      {/if}
    </div>
  </div>

  {#if unifiedItems.length === 0}
    <div class="empty">{t("events.empty")}</div>
  {:else if filteredItems.length === 0}
    <div class="empty">{t("events.emptyFiltered")}</div>
  {:else}
    <div class="feed-scroll">
      {#if pinnedItems.length > 0}
        <div class="section-label critical">
          <span>⚠ {t("events.pinnedSection")} ({pinnedItems.length})</span>
        </div>
        <ul class="feed-list pinned-list">
          {#each pinnedItems as item (item.id + item.timestamp)}
            <li>
              <button
                type="button"
                class="feed-item sev-{item.severity}"
                onclick={() => {
                  if (item.latitude !== undefined && item.longitude !== undefined) {
                    focusMapAt(item.latitude, item.longitude, item.zoom);
                  }
                }}
              >
                <div class="item-top">
                  <div class="badge-group">
                    <span class="sev-badge {item.severity}">
                      {getSeveritySymbol(item.severity)} {getSeverityLabel(item.severity)}
                    </span>
                    <span class="cat-badge">{item.typeLabel}</span>
                    {#each item.badges as b}
                      <span class="metric-badge {item.severity}">{b}</span>
                    {/each}
                  </div>
                  <span class="time">{formatTime(item.timestamp)}</span>
                </div>
                <div class="title">{item.title}</div>
                {#if item.detail}
                  <div class="detail">{item.detail}</div>
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      {/if}

      <ul class="feed-list">
        {#each regularItems as item (item.id + item.timestamp)}
          <li>
            <button
              type="button"
              class="feed-item sev-{item.severity}"
              onclick={() => {
                if (item.latitude !== undefined && item.longitude !== undefined) {
                  focusMapAt(item.latitude, item.longitude, item.zoom);
                }
              }}
            >
              <div class="item-top">
                <div class="badge-group">
                  <span class="sev-badge {item.severity}">
                    {getSeveritySymbol(item.severity)} {getSeverityLabel(item.severity)}
                  </span>
                  <span class="cat-badge">{item.typeLabel}</span>
                  {#each item.badges as b}
                    <span class="metric-badge {item.severity}">{b}</span>
                  {/each}
                </div>
                <span class="time">{formatTime(item.timestamp)}</span>
              </div>
              <div class="title">{item.title}</div>
              {#if item.detail}
                <div class="detail">{item.detail}</div>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</div>

<style>
  .feed-wrap {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .feed-header {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px 8px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.14);
  }
  .header-title-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.8rem;
    color: #94a3b8;
  }
  .header-title-row h3 {
    margin: 0;
    font-size: 0.86rem;
    color: #e2e8f0;
  }
  .total-count {
    font-size: 0.75rem;
    color: #94a3b8;
    font-variant-numeric: tabular-nums;
  }
  .summary-pills {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .summary-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 0.72rem;
    border: 1px solid rgba(148, 163, 184, 0.2);
    background: rgba(15, 23, 42, 0.6);
    color: #cbd5e1;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .summary-pill strong {
    font-variant-numeric: tabular-nums;
  }
  .summary-pill.critical {
    border-color: rgba(239, 68, 68, 0.4);
    color: #fca5a5;
  }
  .summary-pill.critical.active {
    background: rgba(239, 68, 68, 0.26);
    border-color: #ef4444;
    color: #ffffff;
  }
  .summary-pill.warning {
    border-color: rgba(245, 158, 11, 0.4);
    color: #fcd34d;
  }
  .summary-pill.warning.active {
    background: rgba(245, 158, 11, 0.24);
    border-color: #f59e0b;
    color: #ffffff;
  }
  .summary-pill.info {
    border-color: rgba(56, 189, 248, 0.3);
    color: #7dd3fc;
  }
  .summary-pill.info.active {
    background: rgba(56, 189, 248, 0.22);
    border-color: #38bdf8;
    color: #ffffff;
  }

  .feed-controls {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 10px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.12);
    background: rgba(7, 11, 23, 0.55);
  }
  .window-chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }
  .chip-btn {
    padding: 3px 8px;
    font-size: 0.72rem;
    border-radius: 6px;
    border: 1px solid rgba(148, 163, 184, 0.2);
    background: #0b1224;
    color: #94a3b8;
    cursor: pointer;
  }
  .chip-btn:hover:not(:disabled) {
    border-color: rgba(56, 189, 248, 0.45);
    color: #e2e8f0;
  }
  .chip-btn.active {
    background: rgba(56, 189, 248, 0.18);
    border-color: #38bdf8;
    color: #e0f2fe;
    font-weight: 600;
  }
  .chip-btn:disabled {
    opacity: 0.5;
    cursor: wait;
  }

  .filter-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 6px;
  }
  .ctrl-select {
    width: 100%;
    padding: 4px 6px;
    font-size: 0.74rem;
    border-radius: 6px;
    border: 1px solid rgba(148, 163, 184, 0.22);
    background: #0b1224;
    color: #e2e8f0;
  }
  .search-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .search-input {
    flex: 1;
    min-width: 130px;
    padding: 4px 8px;
    font-size: 0.75rem;
    border-radius: 6px;
    border: 1px solid rgba(148, 163, 184, 0.22);
    background: #0b1224;
    color: #e2e8f0;
  }
  .search-input::placeholder {
    color: #64748b;
  }
  .pin-toggle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 0.72rem;
    color: #cbd5e1;
    cursor: pointer;
    user-select: none;
  }
  .reset-btn {
    padding: 3px 8px;
    font-size: 0.72rem;
    border-radius: 6px;
    border: 1px solid rgba(148, 163, 184, 0.25);
    background: rgba(30, 41, 59, 0.65);
    color: #cbd5e1;
    cursor: pointer;
  }

  .empty {
    margin: auto;
    padding: 24px 12px;
    color: #64748b;
    font-size: 0.84rem;
    text-align: center;
  }
  .feed-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 8px;
  }
  .section-label {
    font-size: 0.73rem;
    font-weight: 700;
    letter-spacing: 0.02em;
    margin: 2px 2px 6px;
    color: #fca5a5;
  }
  .feed-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .pinned-list {
    margin-bottom: 10px;
    padding-bottom: 10px;
    border-bottom: 1px dashed rgba(239, 68, 68, 0.35);
  }

  .feed-item {
    width: 100%;
    text-align: left;
    background: #070b17;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-left-width: 3px;
    border-radius: 8px;
    padding: 8px 10px;
    color: #e2e8f0;
    cursor: pointer;
    transition: border-color 0.15s ease, transform 0.1s ease;
  }
  .feed-item:hover {
    border-color: rgba(56, 189, 248, 0.5);
  }
  .feed-item.sev-critical {
    border-color: rgba(239, 68, 68, 0.45);
    border-left: 4px solid #ef4444;
    background: linear-gradient(90deg, rgba(239, 68, 68, 0.18) 0%, rgba(15, 23, 42, 0.92) 60%);
  }
  .feed-item.sev-critical:hover {
    border-color: #f87171;
  }
  .feed-item.sev-warning {
    border-color: rgba(245, 158, 11, 0.32);
    border-left: 3px solid #f59e0b;
    background: linear-gradient(90deg, rgba(245, 158, 11, 0.1) 0%, rgba(7, 11, 23, 0.96) 55%);
  }
  .feed-item.sev-warning:hover {
    border-color: #fbbf24;
  }
  .feed-item.sev-info {
    border-left: 3px solid rgba(56, 189, 248, 0.4);
  }

  .item-top {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 6px;
    margin-bottom: 5px;
  }
  .badge-group {
    display: flex;
    align-items: center;
    gap: 5px;
    flex-wrap: wrap;
  }
  .sev-badge {
    font-size: 0.68rem;
    font-weight: 700;
    padding: 1px 6px;
    border-radius: 4px;
    letter-spacing: 0.02em;
  }
  .sev-badge.critical {
    background: #dc2626;
    color: #ffffff;
  }
  .sev-badge.warning {
    background: rgba(245, 158, 11, 0.24);
    color: #fcd34d;
    border: 1px solid rgba(245, 158, 11, 0.45);
  }
  .sev-badge.info {
    background: rgba(56, 189, 248, 0.14);
    color: #7dd3fc;
  }
  .cat-badge {
    font-size: 0.68rem;
    padding: 1px 6px;
    border-radius: 4px;
    background: rgba(148, 163, 184, 0.14);
    color: #cbd5e1;
  }
  .metric-badge {
    font-size: 0.68rem;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 4px;
    background: rgba(30, 41, 59, 0.85);
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.22);
  }
  .metric-badge.critical {
    background: rgba(127, 29, 29, 0.7);
    border-color: rgba(248, 113, 113, 0.5);
    color: #fecaca;
  }
  .metric-badge.warning {
    background: rgba(120, 53, 15, 0.55);
    border-color: rgba(251, 191, 36, 0.4);
    color: #fde68a;
  }
  .time {
    font-size: 0.71rem;
    color: #94a3b8;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .title {
    font-size: 0.84rem;
    font-weight: 600;
    line-height: 1.35;
  }
  .feed-item.sev-critical .title {
    color: #ffffff;
    font-size: 0.87rem;
  }
  .detail {
    font-size: 0.75rem;
    color: #94a3b8;
    margin-top: 3px;
  }
</style>
