<script lang="ts">
  import { onMount } from "svelte";
  import * as echarts from "echarts";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type { HistoryResult } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import {
    appState,
    getWindowInitialParams,
    getWindowView,
    openAnalysisWindowWithOptions,
    selectMarineLocation,
  } from "../stores/appState.svelte";
  import { locDisplayName, t } from "../i18n/index.svelte";

  type MarineMode = "compare" | "trend";
  type MarineRangeKey = "7d" | "30d" | "90d" | "1y" | "3y" | "5y" | "all";
  type MarineGranularity = "auto" | "hourly" | "daily" | "monthly" | "yearly";
  type MarineMetric = "seaSurfaceTemperature" | "waveHeight" | "oceanCurrentVelocity";

  const isMarineWindow = getWindowView() === "marine";
  const initialParams = isMarineWindow ? getWindowInitialParams() : null;

  const validModes: MarineMode[] = ["compare", "trend"];
  const validRanges: MarineRangeKey[] = ["7d", "30d", "90d", "1y", "3y", "5y", "all"];
  const validGranularities: MarineGranularity[] = ["auto", "hourly", "daily", "monthly", "yearly"];
  const validMetrics: MarineMetric[] = ["seaSurfaceTemperature", "waveHeight", "oceanCurrentVelocity"];

  let chartEl: HTMLDivElement;
  let chart: echarts.ECharts | null = null;
  let mode = $state<MarineMode>(
    initialParams?.mode && validModes.includes(initialParams.mode as MarineMode)
      ? (initialParams.mode as MarineMode)
      : appState.platform.isMobile
        ? "compare"
        : "trend"
  );
  let rangeKey = $state<MarineRangeKey>(
    initialParams?.rangeKey && validRanges.includes(initialParams.rangeKey as MarineRangeKey)
      ? (initialParams.rangeKey as MarineRangeKey)
      : "30d"
  );
  let granularity = $state<MarineGranularity>(
    initialParams?.granularity && validGranularities.includes(initialParams.granularity as MarineGranularity)
      ? (initialParams.granularity as MarineGranularity)
      : "auto"
  );
  let metric = $state<MarineMetric>(
    initialParams?.metric && validMetrics.includes(initialParams.metric as MarineMetric)
      ? (initialParams.metric as MarineMetric)
      : "seaSurfaceTemperature"
  );
  let showMovingAvg = $state(initialParams?.showMovingAvg ?? true);
  let historyResult = $state<HistoryResult | null>(null);
  let categoryFilter = $state<"all" | "regional" | "climate">("all");

  const filteredMarineLocations = $derived.by(() => {
    const locs = appState.config?.marineLocations ?? [];
    if (categoryFilter === "climate") {
      return locs.filter((l) => l.category === "climate");
    }
    if (categoryFilter === "regional") {
      return locs.filter((l) => l.category !== "climate");
    }
    return locs;
  });

  function marineCatLabel(cat?: string): string {
    switch (cat) {
      case "nearshore":
        return t("location.marineCatNearshore");
      case "climate":
        return t("location.marineCatClimate");
      default:
        return t("location.marineCatRegional");
    }
  }

  const syncStatus = $derived(
    appState.historySyncStatuses.find(
      (s) => s.kind === "marine" && s.locationId === appState.selectedMarineId
    ) ?? null
  );

  function formatKey(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return (
      String(d.getUTCFullYear()) +
      pad(d.getUTCMonth() + 1) +
      pad(d.getUTCDate()) +
      pad(d.getUTCHours()) +
      pad(d.getUTCMinutes())
    );
  }

  function rangeStartDate(): Date | null {
    const now = new Date();
    switch (rangeKey) {
      case "7d":
        return new Date(now.getTime() - 7 * 24 * 3600 * 1000);
      case "30d":
        return new Date(now.getTime() - 30 * 24 * 3600 * 1000);
      case "90d":
        return new Date(now.getTime() - 90 * 24 * 3600 * 1000);
      case "1y":
        return new Date(now.getTime() - 365 * 24 * 3600 * 1000);
      case "3y":
        return new Date(now.getTime() - 3 * 365 * 24 * 3600 * 1000);
      case "5y":
        return new Date(now.getTime() - 5 * 365 * 24 * 3600 * 1000);
      default:
        return null;
    }
  }

  function metricMeta(m: typeof metric): { label: string; unit: string; color: string } {
    switch (m) {
      case "waveHeight":
        return { label: t("metrics.waveHeight"), unit: "m", color: "#38bdf8" };
      case "oceanCurrentVelocity":
        return { label: t("metrics.currentVel"), unit: "m/s", color: "#a78bfa" };
      default:
        return { label: t("metrics.sst"), unit: "°C", color: "#2dd4bf" };
    }
  }

  async function loadHistory() {
    if (mode !== "trend") {
      renderChart();
      return;
    }
    const locId = appState.selectedMarineId || appState.config?.marineLocations?.[0]?.id;
    if (!locId) return;
    const now = new Date();
    const from = rangeStartDate();
    try {
      historyResult = await AppService.QueryHistory({
        kind: "marine",
        locationId: locId,
        fromTs: from ? formatKey(from) : "",
        toTs: formatKey(now),
        metric,
        granularity,
        movingAvgWindow: 0,
      });
      renderChart();
    } catch {
      // Ignore transient errors
    }
  }

  function renderChart() {
    if (!chart) return;

    if (mode === "compare") {
      const locs = filteredMarineLocations;
      const items = locs
        .map((loc) => {
          const m = appState.marines.find((snap) => snap.locationId === loc.id);
          if (!m) return null;
          return {
            name: locDisplayName(loc),
            sst: Number(m.seaSurfaceTemperature.toFixed(1)),
            wave: Number(m.waveHeight.toFixed(2)),
            current: Number(m.oceanCurrentVelocity.toFixed(2)),
          };
        })
        .filter((item): item is { name: string; sst: number; wave: number; current: number } => item !== null);

      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: {
            data: [t("metrics.sst"), t("metrics.waveHeight")],
            textStyle: { color: "#94a3b8", fontSize: 11 },
            top: 2,
          },
          grid: { left: 42, right: 42, top: 38, bottom: 28 },
          xAxis: {
            type: "category",
            data: items.map((i) => i.name),
            axisLabel: { color: "#94a3b8", fontSize: 10 },
          },
          yAxis: [
            {
              type: "value",
              name: "°C",
              scale: true,
              axisLabel: { color: "#2dd4bf", fontSize: 10 },
              splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
            },
            {
              type: "value",
              name: "m",
              axisLabel: { color: "#38bdf8", fontSize: 10 },
              splitLine: { show: false },
            },
          ],
          series: [
            {
              name: t("metrics.sst"),
              type: "bar",
              data: items.map((i) => i.sst),
              itemStyle: { color: "rgba(45, 212, 191, 0.78)", borderRadius: [4, 4, 0, 0] },
            },
            {
              name: t("metrics.waveHeight"),
              type: "line",
              yAxisIndex: 1,
              smooth: true,
              data: items.map((i) => i.wave),
              lineStyle: { color: "#38bdf8", width: 2.5 },
              itemStyle: { color: "#38bdf8" },
            },
          ],
        },
        true
      );
      return;
    }

    const pts = historyResult?.points ?? [];
    const stats = historyResult?.stats;
    const meta = metricMeta(metric);
    const series: any[] = [
      {
        name: meta.label,
        type: "line",
        smooth: true,
        showSymbol: pts.length <= 60,
        data: pts.map((p) => p.value),
        lineStyle: { color: meta.color, width: 2.2 },
        itemStyle: { color: meta.color },
        areaStyle: { color: "rgba(45, 212, 191, 0.1)" },
        markLine:
          stats && stats.count > 0
            ? {
                silent: true,
                symbol: "none",
                label: {
                  color: "#cbd5e1",
                  fontSize: 10,
                  formatter: `${t("charts.statMean")}: ${stats.mean.toFixed(1)}`,
                },
                lineStyle: { color: "rgba(248, 250, 252, 0.45)", type: "dotted" },
                data: [{ yAxis: stats.mean }],
              }
            : undefined,
      },
    ];

    if (showMovingAvg && pts.some((p) => p.movingAvg != null)) {
      series.push({
        name: t("charts.movingAvg"),
        type: "line",
        smooth: true,
        showSymbol: false,
        data: pts.map((p) => p.movingAvg ?? null),
        lineStyle: { color: "#fbbf24", width: 2, type: "dashed" },
        itemStyle: { color: "#fbbf24" },
      });
    }

    chart.setOption(
      {
        backgroundColor: "transparent",
        tooltip: { trigger: "axis" },
        legend: {
          data: series.map((s) => s.name),
          textStyle: { color: "#94a3b8", fontSize: 11 },
          top: 2,
        },
        grid: { left: 44, right: 20, top: 36, bottom: 26 },
        xAxis: {
          type: "category",
          data: pts.map((p) => p.label),
          axisLabel: { color: "#64748b", fontSize: 10 },
        },
        yAxis: {
          type: "value",
          name: meta.unit,
          scale: true,
          axisLabel: { color: meta.color, fontSize: 10 },
          splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
        },
        series,
      },
      true
    );
  }

  onMount(() => {
    chart = echarts.init(chartEl);
    void loadHistory();
    const ro = new ResizeObserver(() => chart?.resize());
    ro.observe(chartEl);
    return () => {
      ro.disconnect();
      chart?.dispose();
      chart = null;
    };
  });

  $effect(() => {
    void appState.marines;
    void appState.config?.marineLocations;
    void appState.selectedMarineId;
    void mode;
    void categoryFilter;
    void rangeKey;
    void granularity;
    void metric;
    void showMovingAvg;
    void t("app.title");
    void loadHistory();
  });

  function rangeLabel(r: MarineRangeKey): string {
    switch (r) {
      case "7d": return t("charts.range7d");
      case "30d": return t("charts.range30d");
      case "90d": return t("charts.range90d");
      case "1y": return t("charts.range1y");
      case "3y": return t("charts.range3y");
      case "5y": return t("charts.range5y");
      default: return t("charts.rangeAll");
    }
  }

  $effect(() => {
    if (!isMarineWindow) return;
    if (mode === "compare") {
      appState.windowSubtitle = t("location.allLocations");
      return;
    }
    const loc = (appState.config?.marineLocations ?? []).find((m) => m.id === appState.selectedMarineId);
    const locName = loc ? locDisplayName(loc) : "";
    appState.windowSubtitle = [locName, rangeLabel(rangeKey), metricMeta(metric).label].filter(Boolean).join(" · ");
  });

  async function openInNewWindow() {
    await openAnalysisWindowWithOptions({
      view: "marine",
      mode,
      marineId: appState.selectedMarineId,
      rangeKey,
      metric,
      granularity,
      showMovingAvg,
    });
  }
</script>

<div class="marine-panel">
  <div class="head">
    <div class="title-row">
      <h3>{mode === "compare" ? t("charts.marineCompare") : t("charts.marineTrend")}</h3>
      {#if !appState.platform.isMobile && syncStatus && mode === "trend"}
        <span class="sync-pill" class:warn={syncStatus.state === "rate_limited"}>
          {#if syncStatus.state === "rate_limited"}
            {t("charts.syncRateLimited")}
          {:else if syncStatus.state === "done"}
            {t("charts.syncDone")} ({syncStatus.earliestDate || "-"}〜)
          {:else}
            {t("charts.syncing")} {syncStatus.progressPct.toFixed(0)}%
          {/if}
        </span>
      {/if}
      {#if !appState.platform.isMobile}
        <div class="title-controls">
          <div class="mode-switch">
            <button class:active={mode === "trend"} onclick={() => (mode = "trend")}>
              {t("location.singleLocation")}
            </button>
            <button class:active={mode === "compare"} onclick={() => (mode = "compare")}>
              {t("location.allLocations")}
            </button>
          </div>
          <button
            class="popout-btn"
            title={t("windows.popout")}
            aria-label={t("windows.popout")}
            onclick={() => openInNewWindow()}
          >
            <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 3h7v7" />
              <path d="M10 14L21 3" />
              <path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5" />
            </svg>
          </button>
        </div>
      {/if}
    </div>

    {#if mode === "compare"}
      <div class="filter-bar">
        <div class="mode-switch">
          <button class:active={categoryFilter === "all"} onclick={() => (categoryFilter = "all")}>
            {t("location.marineFilterAll")}
          </button>
          <button class:active={categoryFilter === "regional"} onclick={() => (categoryFilter = "regional")}>
            {t("location.marineFilterRegional")}
          </button>
          <button class:active={categoryFilter === "climate"} onclick={() => (categoryFilter = "climate")}>
            {t("location.marineFilterClimate")}
          </button>
        </div>
      </div>
    {:else if !appState.platform.isMobile && mode === "trend"}
      <div class="filter-bar">
        <select bind:value={metric}>
          <option value="seaSurfaceTemperature">{t("metrics.sst")}</option>
          <option value="waveHeight">{t("metrics.waveHeight")}</option>
          <option value="oceanCurrentVelocity">{t("metrics.currentVel")}</option>
        </select>
        <select bind:value={rangeKey}>
          <option value="7d">{t("charts.range7d")}</option>
          <option value="30d">{t("charts.range30d")}</option>
          <option value="90d">{t("charts.range90d")}</option>
          <option value="1y">{t("charts.range1y")}</option>
          <option value="3y">{t("charts.range3y")}</option>
          <option value="5y">{t("charts.range5y")}</option>
          <option value="all">{t("charts.rangeAll")}</option>
        </select>
        <select bind:value={granularity}>
          <option value="auto">{t("charts.granAuto")}</option>
          <option value="hourly">{t("charts.granHourly")}</option>
          <option value="daily">{t("charts.granDaily")}</option>
          <option value="monthly">{t("charts.granMonthly")}</option>
          <option value="yearly">{t("charts.granYearly")}</option>
        </select>
        <label class="ma-toggle">
          <input type="checkbox" bind:checked={showMovingAvg} />
          <span>{t("charts.movingAvg")}</span>
        </label>
      </div>
    {/if}
  </div>

  <div class="chart-box" bind:this={chartEl}></div>

  {#if mode === "trend" && historyResult && historyResult.stats.count > 0}
    {@const st = historyResult.stats}
    {@const u = metricMeta(metric).unit}
    <div class="stats-strip">
      <div class="stat-pill"><span>{t("charts.statMean")}</span><strong>{st.mean.toFixed(2)} {u}</strong></div>
      <div class="stat-pill"><span>{t("charts.statMin")}</span><strong>{st.min.toFixed(2)} {u}</strong></div>
      <div class="stat-pill"><span>{t("charts.statMax")}</span><strong>{st.max.toFixed(2)} {u}</strong></div>
      <div class="stat-pill"><span>{t("charts.statStdDev")}</span><strong>{st.stdDev.toFixed(2)}</strong></div>
      <div class="stat-pill">
        <span>{t("charts.statTrend")}</span>
        <strong>{st.trendPerDay >= 0 ? "+" : ""}{st.trendPerDay.toFixed(3)}</strong>
      </div>
      <div class="stat-pill"><span>{t("charts.statCount")}</span><strong>{st.count}</strong></div>
    </div>
  {/if}

  <div class="marine-list">
    {#each filteredMarineLocations as ml (ml.id)}
      {@const snap = appState.marines.find((m) => m.locationId === ml.id)}
      {@const selected = ml.id === appState.selectedMarineId}
      <button class="marine-card" class:selected class:climate={ml.category === "climate"} onclick={() => selectMarineLocation(ml.id, true)}>
        <div class="m-top">
          <span class="m-title">{locDisplayName(ml)}</span>
          <span class="m-badges">
            <span class="m-cat" class:climate={ml.category === "climate"} class:nearshore={ml.category === "nearshore"}>
              {marineCatLabel(ml.category)}
            </span>
            <span class="m-coord">{ml.latitude.toFixed(2)}, {ml.longitude.toFixed(2)}</span>
          </span>
        </div>
        {#if snap}
          <div class="m-stats">
            <span class="sst">{t("metrics.sst")}: <strong>{snap.seaSurfaceTemperature.toFixed(1)} °C</strong></span>
            <span>{t("metrics.waveHeight")}: {snap.waveHeight.toFixed(2)} m</span>
            <span>{t("metrics.currentVel")}: {snap.oceanCurrentVelocity.toFixed(2)} m/s</span>
          </div>
        {:else}
          <div class="m-stats muted">{t("app.noData")}</div>
        {/if}
      </button>
    {/each}
  </div>
</div>

<style>
  .marine-panel {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    height: 100%;
    box-sizing: border-box;
    overflow-y: auto;
  }
  .head {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .title-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .title-controls {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .popout-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: rgba(15, 23, 42, 0.8);
    color: #94a3b8;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 4px 6px;
    cursor: pointer;
  }
  .popout-btn:hover {
    border-color: rgba(45, 212, 191, 0.6);
    color: #2dd4bf;
  }
  h3 {
    margin: 0;
    font-size: 0.92rem;
    color: #e2e8f0;
  }
  .sync-pill {
    font-size: 0.68rem;
    padding: 2px 7px;
    border-radius: 999px;
    background: rgba(45, 212, 191, 0.14);
    border: 1px solid rgba(45, 212, 191, 0.35);
    color: #5eead4;
  }
  .sync-pill.warn {
    background: rgba(251, 191, 36, 0.14);
    border-color: rgba(251, 191, 36, 0.4);
    color: #fcd34d;
  }
  .mode-switch {
    display: inline-flex;
    background: rgba(15, 23, 42, 0.8);
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    overflow: hidden;
  }
  .mode-switch button {
    background: transparent;
    color: #94a3b8;
    border: none;
    padding: 4px 8px;
    font-size: 0.72rem;
    cursor: pointer;
  }
  .mode-switch button.active {
    background: rgba(45, 212, 191, 0.2);
    color: #2dd4bf;
    font-weight: 600;
  }
  .filter-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
  .filter-bar select {
    background: rgba(15, 23, 42, 0.85);
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    color: #e2e8f0;
    font-size: 0.72rem;
    padding: 3px 6px;
  }
  .ma-toggle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 0.72rem;
    color: #cbd5e1;
    cursor: pointer;
  }
  .chart-box {
    width: 100%;
    min-height: 220px;
    flex: 0 0 220px;
  }
  .stats-strip {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .stat-pill {
    display: inline-flex;
    align-items: baseline;
    gap: 4px;
    padding: 3px 7px;
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.75);
    border: 1px solid rgba(45, 212, 191, 0.22);
    font-size: 0.68rem;
    color: #94a3b8;
  }
  .stat-pill strong {
    color: #f1f5f9;
    font-size: 0.74rem;
  }
  .marine-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 8px;
  }
  .marine-card {
    background: rgba(15, 23, 42, 0.72);
    border: 1px solid rgba(45, 212, 191, 0.22);
    border-radius: 10px;
    padding: 10px 12px;
    text-align: left;
    color: #cbd5e1;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .marine-card.selected {
    border-color: #2dd4bf;
    background: rgba(13, 148, 136, 0.16);
  }
  .marine-card.climate {
    border-color: rgba(245, 158, 11, 0.28);
  }
  .marine-card.climate.selected {
    border-color: #fbbf24;
    background: rgba(245, 158, 11, 0.14);
  }
  .m-top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }
  .m-title {
    font-weight: 600;
    font-size: 0.85rem;
    color: #99f6e4;
  }
  .marine-card.climate .m-title {
    color: #fde68a;
  }
  .m-badges {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }
  .m-cat {
    font-size: 0.62rem;
    padding: 1px 5px;
    border-radius: 999px;
    background: rgba(148, 163, 184, 0.16);
    color: #cbd5e1;
  }
  .m-cat.nearshore {
    background: rgba(45, 212, 191, 0.16);
    color: #5eead4;
  }
  .m-cat.climate {
    background: rgba(245, 158, 11, 0.16);
    color: #fcd34d;
  }
  .m-coord {
    font-size: 0.7rem;
    color: #64748b;
  }
  .m-stats {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    font-size: 0.76rem;
  }
  .sst strong {
    color: #2dd4bf;
  }
  .muted {
    color: #64748b;
  }
</style>
