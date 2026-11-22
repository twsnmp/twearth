<script lang="ts">
  import { onMount } from "svelte";
  import * as echarts from "echarts";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type {
    DisasterHistoryResult,
    HistoryResult,
    QuakeHistoryResult,
    RiverHistoryResult,
  } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import {
    appState,
    getActiveWeather,
    getWindowInitialParams,
    getWindowView,
    openAnalysisWindowWithOptions,
    queryDisasterHistory,
    queryRiverHistory,
    selectLocation,
  } from "../stores/appState.svelte";
  import { locDisplayName, t } from "../i18n/index.svelte";

  let { horizontal = false }: { horizontal?: boolean } = $props();

  type ChartMode = "compare" | "trend" | "quake" | "disaster" | "river";
  type RangeKey = "24h" | "7d" | "30d" | "90d" | "1y" | "3y" | "5y" | "10y" | "30y" | "all";
  type Granularity = "auto" | "hourly" | "daily" | "monthly" | "yearly";
  type ChartMetric = "temperature2m" | "pressureMsl" | "precipitation" | "snowfall" | "cape" | "kpIndex";

  const isChartsWindow = getWindowView() === "charts";
  const initialParams = isChartsWindow ? getWindowInitialParams() : null;

  const validModes: ChartMode[] = ["compare", "trend", "quake", "disaster", "river"];
  const validRanges: RangeKey[] = ["24h", "7d", "30d", "90d", "1y", "3y", "5y", "10y", "30y", "all"];
  const validGranularities: Granularity[] = ["auto", "hourly", "daily", "monthly", "yearly"];
  const validMetrics: ChartMetric[] = ["temperature2m", "pressureMsl", "precipitation", "snowfall", "cape", "kpIndex"];

  let chartEl: HTMLDivElement;
  let chart: echarts.ECharts | null = null;
  let mode = $state<ChartMode>(
    initialParams?.mode && validModes.includes(initialParams.mode as ChartMode)
      ? (initialParams.mode as ChartMode)
      : "compare"
  );
  let rangeKey = $state<RangeKey>(
    initialParams?.rangeKey && validRanges.includes(initialParams.rangeKey as RangeKey)
      ? (initialParams.rangeKey as RangeKey)
      : "30d"
  );
  let granularity = $state<Granularity>(
    initialParams?.granularity && validGranularities.includes(initialParams.granularity as Granularity)
      ? (initialParams.granularity as Granularity)
      : "auto"
  );
  let metric = $state<ChartMetric>(
    initialParams?.metric && validMetrics.includes(initialParams.metric as ChartMetric)
      ? (initialParams.metric as ChartMetric)
      : "temperature2m"
  );
  let showMovingAvg = $state(initialParams?.showMovingAvg ?? true);
  let quakeRadiusKm = $state<number>(initialParams?.quakeRadiusKm ?? 300);

  let historyResult = $state<HistoryResult | null>(null);
  let quakeResult = $state<QuakeHistoryResult | null>(null);
  let disasterResult = $state<DisasterHistoryResult | null>(null);
  let riverResult = $state<RiverHistoryResult | null>(null);
  let disasterRadiusKm = $state<number>(100);
  let disasterCategory = $state<string>("all");
  let selectedRiverStationId = $state<string>("");

  const activeWeather = $derived(getActiveWeather());
  const selectedLoc = $derived(
    (appState.config?.locations ?? []).find((l) => l.id === appState.selectedLocationId) ??
      appState.config?.locations?.[0] ??
      null
  );
  const syncStatus = $derived(
    appState.historySyncStatuses.find(
      (s) => s.kind === (mode === "quake" ? "quake" : "weather") && (mode === "quake" || s.locationId === appState.selectedLocationId)
    ) ?? null
  );

  function pickCompareWeathers() {
    const locs = appState.config?.locations ?? [];
    const weathers = appState.weathers;
    if (weathers.length <= 5) return weathers;

    const orderMap = new Map<string, number>();
    locs.forEach((l, idx) => orderMap.set(l.id, idx));

    const scored = weathers.map((w, idx) => {
      const loc = locs.find((l) => l.id === w.locationId);
      let priority = 0;
      if (loc?.source === "current") priority += 100;
      if (w.locationId === appState.selectedLocationId) priority += 50;
      if (loc?.isPrimary) priority += 25;
      return { w, priority, order: orderMap.get(w.locationId) ?? 1000 + idx };
    });

    scored.sort((a, b) => (b.priority !== a.priority ? b.priority - a.priority : a.order - b.order));
    return scored
      .slice(0, 5)
      .sort((a, b) => a.order - b.order)
      .map((s) => s.w);
  }

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
      case "24h":
        return new Date(now.getTime() - 24 * 3600 * 1000);
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
      case "10y":
        return new Date(now.getTime() - 10 * 365 * 24 * 3600 * 1000);
      case "30y":
        return new Date(now.getTime() - 30 * 365 * 24 * 3600 * 1000);
      default:
        return null;
    }
  }

  function metricMeta(m: typeof metric): { label: string; unit: string; color: string; isBar?: boolean } {
    switch (m) {
      case "pressureMsl":
        return { label: t("metrics.pressureMsl"), unit: "hPa", color: "#38bdf8" };
      case "precipitation":
        return { label: t("metrics.precipitation"), unit: "mm", color: "#60a5fa", isBar: true };
      case "snowfall":
        return { label: t("metrics.snowfall"), unit: "cm", color: "#e0f2fe", isBar: true };
      case "cape":
        return { label: t("metrics.cape"), unit: "J/kg", color: "#f43f5e", isBar: true };
      case "kpIndex":
        return { label: t("metrics.kpIndex"), unit: "Kp", color: "#c084fc" };
      default:
        return { label: t("metrics.temperature"), unit: "°C", color: "#fbbf24" };
    }
  }

  async function loadHistory() {
    if (mode === "trend") {
      const locId = appState.selectedLocationId;
      if (!locId) return;
      const now = new Date();
      const from = rangeStartDate();
      try {
        historyResult = await AppService.QueryHistory({
          kind: "weather",
          locationId: locId,
          fromTs: from ? formatKey(from) : "",
          toTs: formatKey(now),
          metric,
          granularity,
          movingAvgWindow: 0,
        });
        renderChart();
      } catch {
        // Ignore transient load errors
      }
      return;
    }

    if (mode === "quake") {
      const now = new Date();
      const from = rangeStartDate() ?? new Date(now.getTime() - 365 * 24 * 3600 * 1000);
      try {
        quakeResult = await AppService.QueryQuakeHistory({
          latitude: selectedLoc?.latitude ?? 35.6762,
          longitude: selectedLoc?.longitude ?? 139.6503,
          radiusKm: quakeRadiusKm,
          fromTime: from.toISOString(),
          toTime: now.toISOString(),
          granularity: "auto",
        });
        renderChart();
      } catch {
        // Ignore transient load errors
      }
      return;
    }

    if (mode === "disaster") {
      const now = new Date();
      const from = rangeStartDate() ?? new Date(now.getTime() - 365 * 24 * 3600 * 1000);
      try {
        disasterResult = await queryDisasterHistory({
          latitude: selectedLoc?.latitude ?? 35.6762,
          longitude: selectedLoc?.longitude ?? 139.6503,
          radiusKm: disasterRadiusKm,
          fromTime: from.toISOString(),
          toTime: now.toISOString(),
          categories: disasterCategory === "all" ? [] : [disasterCategory],
          granularity: "auto",
        });
        renderChart();
      } catch {
        // Ignore transient load errors
      }
      return;
    }

    if (mode === "river") {
      const stId = selectedRiverStationId || appState.rivers[0]?.stationId || "";
      if (!stId) {
        riverResult = null;
        renderChart();
        return;
      }
      if (!selectedRiverStationId) selectedRiverStationId = stId;
      const now = new Date();
      const from = rangeStartDate() ?? new Date(now.getTime() - 7 * 24 * 3600 * 1000);
      try {
        riverResult = await queryRiverHistory({
          stationId: stId,
          fromTime: from.toISOString(),
          toTime: now.toISOString(),
        });
        renderChart();
      } catch {
        // Ignore transient load errors
      }
    }
  }

  function renderChart() {
    if (!chart) return;

    if (mode === "compare" && appState.weathers.length > 0) {
      const locs = appState.config?.locations ?? [];
      const items = pickCompareWeathers().map((w) => {
        const loc = locs.find((l) => l.id === w.locationId);
        return {
          name: loc ? locDisplayName(loc) : w.locationName,
          temp: Number(w.temperature2m.toFixed(1)),
          pressure: Number(w.pressureMsl.toFixed(1)),
          cape: Math.round(w.cape),
          precip: Number(w.precipitation.toFixed(1)),
          snowfall: Number(w.snowfall.toFixed(1)),
        };
      });

      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: {
            type: "scroll",
            data: [
              t("metrics.temperature"),
              t("metrics.pressureMsl"),
              t("metrics.precipitation"),
              t("metrics.snowfall"),
              t("metrics.cape"),
            ],
            textStyle: { color: "#94a3b8", fontSize: 10 },
            top: 0,
          },
          grid: { left: 38, right: 42, top: 32, bottom: 22 },
          xAxis: {
            type: "category",
            data: items.map((i) => i.name),
            axisLabel: { color: "#94a3b8", fontSize: 10, interval: 0, overflow: "truncate", width: 72 },
          },
          yAxis: [
            {
              type: "value",
              name: "°C",
              axisLabel: { color: "#fbbf24", fontSize: 10 },
              splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
            },
            {
              type: "value",
              name: "hPa",
              scale: true,
              axisLabel: { color: "#38bdf8", fontSize: 10 },
              splitLine: { show: false },
            },
          ],
          series: [
            {
              name: t("metrics.temperature"),
              type: "bar",
              data: items.map((i) => i.temp),
              itemStyle: { color: "rgba(251, 191, 36, 0.78)", borderRadius: [4, 4, 0, 0] },
            },
            {
              name: t("metrics.pressureMsl"),
              type: "line",
              yAxisIndex: 1,
              smooth: true,
              data: items.map((i) => i.pressure),
              lineStyle: { color: "#38bdf8", width: 2.5 },
              itemStyle: { color: "#38bdf8" },
            },
            {
              name: t("metrics.precipitation"),
              type: "bar",
              data: items.map((i) => i.precip),
              itemStyle: { color: "rgba(96, 165, 250, 0.8)", borderRadius: [4, 4, 0, 0] },
            },
            {
              name: t("metrics.snowfall"),
              type: "bar",
              data: items.map((i) => i.snowfall),
              itemStyle: { color: "rgba(224, 242, 254, 0.85)", borderRadius: [4, 4, 0, 0] },
            },
            {
              name: t("metrics.cape"),
              type: "bar",
              data: items.map((i) => i.cape),
              itemStyle: { color: "rgba(244, 63, 94, 0.45)" },
            },
          ],
        },
        true
      );
      return;
    }

    if (mode === "trend") {
      const pts = historyResult?.points ?? [];
      const stats = historyResult?.stats;
      const meta = metricMeta(metric);
      const labels = pts.map((p) => p.label);
      const series: any[] = [
        {
          name: meta.label,
          type: meta.isBar ? "bar" : "line",
          smooth: !meta.isBar,
          showSymbol: pts.length <= 60,
          data: pts.map((p) => p.value),
          lineStyle: { color: meta.color, width: 2 },
          itemStyle: { color: meta.color },
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
          lineStyle: { color: "#f472b6", width: 2, type: "dashed" },
          itemStyle: { color: "#f472b6" },
        });
      }

      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: {
            data: series.map((s) => s.name),
            textStyle: { color: "#94a3b8", fontSize: 10 },
            top: 2,
          },
          grid: { left: 42, right: 20, top: 34, bottom: 26 },
          xAxis: {
            type: "category",
            data: labels,
            axisLabel: { color: "#64748b", fontSize: 10 },
          },
          yAxis: {
            type: "value",
            name: meta.unit,
            scale: !meta.isBar,
            axisLabel: { color: meta.color, fontSize: 10 },
            splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
          },
          series,
        },
        true
      );
      return;
    }

    if (mode === "quake") {
      const buckets = quakeResult?.buckets ?? [];
      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: {
            data: [t("charts.statCount"), "Max M"],
            textStyle: { color: "#94a3b8", fontSize: 10 },
            top: 2,
          },
          grid: { left: 38, right: 38, top: 34, bottom: 26 },
          xAxis: {
            type: "category",
            data: buckets.map((b) => b.label),
            axisLabel: { color: "#64748b", fontSize: 10 },
          },
          yAxis: [
            {
              type: "value",
              name: "#",
              minInterval: 1,
              axisLabel: { color: "#fb7185", fontSize: 10 },
              splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
            },
            {
              type: "value",
              name: "M",
              min: 0,
              max: 9,
              axisLabel: { color: "#fbbf24", fontSize: 10 },
              splitLine: { show: false },
            },
          ],
          series: [
            {
              name: t("charts.statCount"),
              type: "bar",
              data: buckets.map((b) => b.count),
              itemStyle: { color: "rgba(251, 113, 133, 0.78)", borderRadius: [4, 4, 0, 0] },
            },
            {
              name: "Max M",
              type: "line",
              yAxisIndex: 1,
              smooth: true,
              data: buckets.map((b) => b.maxMagnitude),
              lineStyle: { color: "#fbbf24", width: 2 },
              itemStyle: { color: "#fbbf24" },
            },
          ],
        },
        true
      );
      return;
    }

    if (mode === "disaster") {
      const buckets = disasterResult?.buckets ?? [];
      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: {
            data: ["Total", "Critical", "Warning"],
            textStyle: { color: "#94a3b8", fontSize: 10 },
            top: 2,
          },
          grid: { left: 38, right: 20, top: 34, bottom: 26 },
          xAxis: {
            type: "category",
            data: buckets.map((b) => b.label),
            axisLabel: { color: "#64748b", fontSize: 10 },
          },
          yAxis: {
            type: "value",
            minInterval: 1,
            axisLabel: { color: "#94a3b8", fontSize: 10 },
            splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
          },
          series: [
            {
              name: "Total",
              type: "bar",
              data: buckets.map((b) => b.count),
              itemStyle: { color: "rgba(56, 189, 248, 0.7)", borderRadius: [3, 3, 0, 0] },
            },
            {
              name: "Warning",
              type: "bar",
              data: buckets.map((b) => b.warningCount),
              itemStyle: { color: "rgba(249, 115, 22, 0.8)", borderRadius: [3, 3, 0, 0] },
            },
            {
              name: "Critical",
              type: "bar",
              data: buckets.map((b) => b.criticalCount),
              itemStyle: { color: "rgba(239, 68, 68, 0.85)", borderRadius: [3, 3, 0, 0] },
            },
          ],
        },
        true
      );
      return;
    }

    if (mode === "river") {
      const pts = riverResult?.points ?? [];
      const markLines: any[] = [];
      if (riverResult && riverResult.warningLevelM > 0) {
        markLines.push({
          yAxis: riverResult.warningLevelM,
          name: "Warning",
          lineStyle: { color: "#f97316", type: "dashed" },
        });
      }
      if (riverResult && riverResult.dangerLevelM > 0) {
        markLines.push({
          yAxis: riverResult.dangerLevelM,
          name: "Danger",
          lineStyle: { color: "#ef4444", type: "dashed" },
        });
      }
      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          grid: { left: 42, right: 20, top: 26, bottom: 26 },
          xAxis: {
            type: "category",
            data: pts.map((p) => p.timestamp.slice(4, 12)),
            axisLabel: { color: "#64748b", fontSize: 10 },
          },
          yAxis: {
            type: "value",
            name: "m",
            axisLabel: { color: "#14b8a6", fontSize: 10 },
            splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
          },
          series: [
            {
              name: riverResult?.stationName || "Level",
              type: "line",
              smooth: true,
              data: pts.map((p) => p.waterLevelM),
              lineStyle: { color: "#14b8a6", width: 2 },
              itemStyle: { color: "#14b8a6" },
              areaStyle: { color: "rgba(20, 184, 166, 0.16)" },
              markLine: markLines.length > 0 ? { data: markLines, symbol: "none" } : undefined,
            },
          ],
        },
        true
      );
    }
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
    void appState.weathers;
    void appState.selectedLocationId;
    void appState.disasters;
    void appState.rivers;
    void mode;
    void rangeKey;
    void granularity;
    void metric;
    void showMovingAvg;
    void quakeRadiusKm;
    void disasterRadiusKm;
    void disasterCategory;
    void selectedRiverStationId;
    void t("app.title");
    if (mode === "trend" || mode === "quake" || mode === "disaster" || mode === "river") {
      void loadHistory();
    } else {
      renderChart();
    }
  });

  function rangeLabel(r: RangeKey): string {
    switch (r) {
      case "24h": return t("charts.range24h");
      case "7d": return t("charts.range7d");
      case "30d": return t("charts.range30d");
      case "90d": return t("charts.range90d");
      case "1y": return t("charts.range1y");
      case "3y": return t("charts.range3y");
      case "5y": return t("charts.range5y");
      case "10y": return t("charts.range10y");
      case "30y": return t("charts.range30y");
      default: return t("charts.rangeAll");
    }
  }

  $effect(() => {
    if (!isChartsWindow) return;
    if (mode === "compare") {
      appState.windowSubtitle = t("location.allLocations");
      return;
    }
    const locName = selectedLoc ? locDisplayName(selectedLoc) : "";
    if (mode === "quake") {
      const scope = quakeRadiusKm > 0 ? `${quakeRadiusKm}km` : t("charts.quakeGlobal");
      appState.windowSubtitle = [locName, rangeLabel(rangeKey), scope].filter(Boolean).join(" · ");
      return;
    }
    appState.windowSubtitle = [locName, rangeLabel(rangeKey), metricMeta(metric).label].filter(Boolean).join(" · ");
  });

  async function openInNewWindow() {
    await openAnalysisWindowWithOptions({
      view: "charts",
      mode,
      locationId: appState.selectedLocationId,
      rangeKey,
      metric,
      granularity,
      showMovingAvg,
      quakeRadiusKm,
    });
  }
</script>

<div class="charts-panel" class:horizontal>
  <div class="panel-header">
    <div class="title-row">
      <h3>
        {#if mode === "compare"}
          {t("charts.multiCityCompare")}
        {:else if mode === "trend"}
          {t("charts.historyTrend")}
        {:else}
          {t("charts.quakeTrend")}
        {/if}
      </h3>
      {#if !appState.platform.isMobile && syncStatus && mode !== "compare"}
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
            <button class:active={mode === "compare"} onclick={() => (mode = "compare")}>
              {t("location.allLocations")}
            </button>
            <button class:active={mode === "trend"} onclick={() => (mode = "trend")}>
              {t("location.singleLocation")}
            </button>
            <button class:active={mode === "quake"} onclick={() => (mode = "quake")}>
              {t("layers.quakes")}
            </button>
            <button class:active={mode === "disaster"} onclick={() => (mode = "disaster")}>
              {t("layers.disasters")}
            </button>
            <button class:active={mode === "river"} onclick={() => (mode = "river")}>
              {t("layers.rivers")}
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

    {#if mode === "trend"}
      <div class="filter-bar">
        <select bind:value={metric}>
          <option value="temperature2m">{t("metrics.temperature")}</option>
          <option value="pressureMsl">{t("metrics.pressureMsl")}</option>
          <option value="precipitation">{t("metrics.precipitation")}</option>
          <option value="snowfall">{t("metrics.snowfall")}</option>
          <option value="cape">{t("metrics.cape")}</option>
          <option value="kpIndex">{t("metrics.kpIndex")}</option>
        </select>
        <select bind:value={rangeKey}>
          <option value="24h">{t("charts.range24h")}</option>
          <option value="7d">{t("charts.range7d")}</option>
          <option value="30d">{t("charts.range30d")}</option>
          <option value="90d">{t("charts.range90d")}</option>
          <option value="1y">{t("charts.range1y")}</option>
          <option value="3y">{t("charts.range3y")}</option>
          <option value="5y">{t("charts.range5y")}</option>
          <option value="10y">{t("charts.range10y")}</option>
          <option value="30y">{t("charts.range30y")}</option>
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
    {:else if mode === "quake"}
      <div class="filter-bar">
        <select bind:value={rangeKey}>
          <option value="7d">{t("charts.range7d")}</option>
          <option value="30d">{t("charts.range30d")}</option>
          <option value="90d">{t("charts.range90d")}</option>
          <option value="1y">{t("charts.range1y")}</option>
          <option value="3y">{t("charts.range3y")}</option>
          <option value="5y">{t("charts.range5y")}</option>
          <option value="10y">{t("charts.range10y")}</option>
          <option value="all">{t("charts.rangeAll")}</option>
        </select>
        <select bind:value={quakeRadiusKm}>
          <option value={100}>{t("charts.quakeRadius100")}</option>
          <option value={300}>{t("charts.quakeRadius300")}</option>
          <option value={0}>{t("charts.quakeGlobal")}</option>
        </select>
      </div>
    {:else if mode === "disaster"}
      <div class="filter-bar">
        <select bind:value={rangeKey}>
          <option value="7d">{t("charts.range7d")}</option>
          <option value="30d">{t("charts.range30d")}</option>
          <option value="90d">{t("charts.range90d")}</option>
          <option value="1y">{t("charts.range1y")}</option>
          <option value="all">{t("charts.rangeAll")}</option>
        </select>
        <select bind:value={disasterRadiusKm}>
          <option value={50}>50 km</option>
          <option value={100}>100 km</option>
          <option value={300}>300 km</option>
          <option value={0}>{t("charts.quakeGlobal")}</option>
        </select>
        <select bind:value={disasterCategory}>
          <option value="all">{t("events.typeAll")}</option>
          <option value="flood">Flood</option>
          <option value="landslide">Landslide</option>
          <option value="snow">Snow / Avalanche</option>
          <option value="wildfire">Wildfire</option>
          <option value="storm">Storm</option>
          <option value="warning">Warning</option>
        </select>
      </div>
    {:else if mode === "river"}
      <div class="filter-bar">
        <select bind:value={selectedRiverStationId}>
          {#if appState.rivers.length === 0}
            <option value="">No river stations</option>
          {:else}
            {#each appState.rivers as r (r.stationId)}
              <option value={r.stationId}>{r.riverName} · {r.stationName} ({r.waterLevelM.toFixed(2)}m)</option>
            {/each}
          {/if}
        </select>
        <select bind:value={rangeKey}>
          <option value="24h">{t("charts.range24h")}</option>
          <option value="7d">{t("charts.range7d")}</option>
          <option value="30d">{t("charts.range30d")}</option>
        </select>
      </div>
    {/if}
  </div>

  <div class="charts-body" class:horizontal>
    <div class="chart-col">
      <div class="chart-box" bind:this={chartEl}></div>
      {#if mode === "trend" && historyResult && historyResult.stats.count > 0}
        {@const st = historyResult.stats}
        {@const u = metricMeta(metric).unit}
        <div class="stats-strip">
          <div class="stat-pill"><span>{t("charts.statMean")}</span><strong>{st.mean.toFixed(1)} {u}</strong></div>
          <div class="stat-pill"><span>{t("charts.statMin")}</span><strong>{st.min.toFixed(1)} {u}</strong></div>
          <div class="stat-pill"><span>{t("charts.statMax")}</span><strong>{st.max.toFixed(1)} {u}</strong></div>
          <div class="stat-pill"><span>{t("charts.statStdDev")}</span><strong>{st.stdDev.toFixed(2)}</strong></div>
          <div class="stat-pill">
            <span>{t("charts.statTrend")}</span>
            <strong>{st.trendPerDay >= 0 ? "+" : ""}{st.trendPerDay.toFixed(3)}</strong>
          </div>
          <div class="stat-pill"><span>{t("charts.statCount")}</span><strong>{st.count}</strong></div>
        </div>
      {:else if mode === "quake" && quakeResult}
        <div class="stats-strip">
          <div class="stat-pill"><span>{t("charts.statCount")}</span><strong>{quakeResult.totalCount}</strong></div>
          <div class="stat-pill"><span>{t("charts.statMax")} M</span><strong>{quakeResult.maxMagnitude.toFixed(1)}</strong></div>
          <div class="stat-pill"><span>{t("charts.statMean")} M</span><strong>{quakeResult.avgMagnitude.toFixed(1)}</strong></div>
          <div class="stat-pill"><span>{t("charts.statMax")}</span><strong>{quakeResult.maxScale || "-"}</strong></div>
        </div>
      {:else if mode === "disaster" && disasterResult}
        <div class="stats-strip">
          <div class="stat-pill"><span>{t("charts.statCount")}</span><strong>{disasterResult.totalCount}</strong></div>
          <div class="stat-pill"><span>Active</span><strong>{disasterResult.activeCount}</strong></div>
          <div class="stat-pill"><span>Critical</span><strong>{disasterResult.criticalCount}</strong></div>
          <div class="stat-pill"><span>Warning</span><strong>{disasterResult.warningCount}</strong></div>
          {#if disasterResult.nearbyLore.totalCount > 0}
            <div class="stat-pill">
              <span>{t("layers.lore")}</span>
              <strong>{disasterResult.nearbyLore.totalCount} ({disasterResult.nearbyLore.earliestYear || "-"})</strong>
            </div>
          {/if}
        </div>
      {:else if mode === "river" && riverResult && (riverResult.points?.length ?? 0) > 0}
        <div class="stats-strip">
          <div class="stat-pill"><span>Latest</span><strong>{riverResult.latestLevelM.toFixed(2)} m</strong></div>
          <div class="stat-pill"><span>{t("charts.statMax")}</span><strong>{riverResult.maxLevelM.toFixed(2)} m</strong></div>
          <div class="stat-pill"><span>{t("charts.statMin")}</span><strong>{riverResult.minLevelM.toFixed(2)} m</strong></div>
          {#if riverResult.dangerLevelM > 0}
            <div class="stat-pill"><span>Danger</span><strong>{riverResult.dangerLevelM.toFixed(2)} m</strong></div>
          {/if}
        </div>
      {/if}
    </div>

    <div class="city-table">
      {#if appState.weathers.length > 0}
        {#each appState.weathers as w (w.locationId)}
          {@const loc = appState.config?.locations?.find((l) => l.id === w.locationId)}
          <button
            class="city-row"
            class:selected={w.locationId === appState.selectedLocationId}
            onclick={() => selectLocation(w.locationId, true)}
          >
            <span class="c-name">
              {loc ? locDisplayName(loc) : w.locationName}
              {#if loc?.source === "current"}
                <span class="gps-tag">GPS</span>
              {/if}
            </span>
            <span>{w.temperature2m.toFixed(1)}°C</span>
            <span>{w.pressureMsl.toFixed(1)} hPa</span>
            <span>{w.relativeHumidity2m.toFixed(0)}%</span>
          </button>
        {/each}
      {/if}
    </div>

    {#if activeWeather}
      <div class="kpi-grid">
        <div class="kpi">
          <span class="k-label">{t("metrics.pressureMsl")}</span>
          <span class="k-val">{activeWeather.pressureMsl.toFixed(1)} hPa</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.pressure3h")}</span>
          <span class="k-val" class:warn={activeWeather.pressureDelta3h <= -2}>
            {activeWeather.pressureDelta3h >= 0 ? "+" : ""}{activeWeather.pressureDelta3h.toFixed(1)} hPa
          </span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.temperature")}</span>
          <span class="k-val">{activeWeather.temperature2m.toFixed(1)} °C</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.humidity")}</span>
          <span class="k-val">{activeWeather.relativeHumidity2m.toFixed(0)} %</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.wind")}</span>
          <span class="k-val">{activeWeather.windSpeed10m.toFixed(1)} m/s</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.precipitation")}</span>
          <span class="k-val">{activeWeather.precipitation.toFixed(1)} mm</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.snowfall")}</span>
          <span class="k-val">{activeWeather.snowfall.toFixed(1)} cm</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.snowDepth")}</span>
          <span class="k-val">{(activeWeather.snowDepth * 100).toFixed(0)} cm</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.cape")}</span>
          <span class="k-val">{activeWeather.cape.toFixed(0)} J/kg</span>
        </div>
        <div class="kpi">
          <span class="k-label">{t("metrics.pm25")}</span>
          <span class="k-val">{activeWeather.pm25.toFixed(1)} µg/m³</span>
        </div>
        {#if appState.space}
          <div class="kpi">
            <span class="k-label">{t("metrics.kpIndex")}</span>
            <span class="k-val">{appState.space.kpIndex.toFixed(1)} ({appState.space.flareClass})</span>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .charts-panel {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    height: 100%;
    box-sizing: border-box;
    overflow-y: auto;
  }
  .charts-panel.horizontal {
    gap: 8px;
    padding: 10px 12px;
    overflow: hidden;
  }
  .panel-header {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .charts-body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    flex: 1;
    min-height: 0;
  }
  .charts-body.horizontal {
    display: grid;
    grid-template-columns: minmax(280px, 1.55fr) minmax(200px, 0.9fr) minmax(220px, 1.05fr);
    gap: 10px;
    align-items: stretch;
  }
  .chart-col {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
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
    border-color: rgba(56, 189, 248, 0.6);
    color: #38bdf8;
  }
  h3 {
    margin: 0;
    font-size: 0.9rem;
    color: #e2e8f0;
  }
  .sync-pill {
    font-size: 0.68rem;
    padding: 2px 7px;
    border-radius: 999px;
    background: rgba(56, 189, 248, 0.14);
    border: 1px solid rgba(56, 189, 248, 0.35);
    color: #7dd3fc;
  }
  .sync-pill.warn {
    background: rgba(251, 191, 36, 0.14);
    border-color: rgba(251, 191, 36, 0.4);
    color: #fcd34d;
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
    border: 1px solid rgba(148, 163, 184, 0.18);
    font-size: 0.68rem;
    color: #94a3b8;
  }
  .stat-pill strong {
    color: #f1f5f9;
    font-size: 0.74rem;
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
    background: rgba(56, 189, 248, 0.2);
    color: #38bdf8;
    font-weight: 600;
  }
  .chart-box {
    width: 100%;
    min-height: 200px;
    flex: 1 0 200px;
  }
  .charts-body.horizontal .chart-box {
    min-height: 0;
    height: 100%;
  }
  .kpi-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
    gap: 6px;
    align-content: start;
  }
  .charts-body.horizontal .kpi-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    overflow-y: auto;
    min-height: 0;
  }
  .kpi {
    background: rgba(15, 23, 42, 0.75);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 8px;
    padding: 6px 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .k-label {
    font-size: 0.68rem;
    color: #94a3b8;
  }
  .k-val {
    font-size: 0.84rem;
    font-weight: 600;
    color: #e2e8f0;
  }
  .k-val.warn {
    color: #fb7185;
  }
  .city-table {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .charts-body.horizontal .city-table {
    overflow-y: auto;
    min-height: 0;
    padding-right: 2px;
  }
  .city-row {
    display: grid;
    grid-template-columns: 1.4fr 0.85fr 1.15fr 0.65fr;
    gap: 6px;
    align-items: center;
    padding: 6px 10px;
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.55);
    border: 1px solid rgba(148, 163, 184, 0.15);
    color: #cbd5e1;
    font-size: 0.76rem;
    cursor: pointer;
    text-align: left;
  }
  .city-row.selected {
    border-color: rgba(56, 189, 248, 0.6);
    background: rgba(14, 116, 144, 0.2);
  }
  .c-name {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-weight: 600;
    color: #f8fafc;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gps-tag {
    font-size: 0.6rem;
    padding: 1px 4px;
    border-radius: 3px;
    background: rgba(16, 185, 129, 0.2);
    color: #34d399;
    flex-shrink: 0;
  }
</style>
