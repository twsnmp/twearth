<script lang="ts">
  import { onMount } from "svelte";
  import * as echarts from "echarts";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type { TimeSeriesPoint } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import { appState, getActiveWeather, selectLocation } from "../stores/appState.svelte";
  import { locDisplayName, t } from "../i18n/index.svelte";

  let chartEl: HTMLDivElement;
  let chart: echarts.ECharts | null = null;
  let series = $state<TimeSeriesPoint[]>([]);
  let mode = $state<"compare" | "trend">("compare");

  const activeWeather = $derived(getActiveWeather());

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

  async function loadSeries() {
    const locId = appState.selectedLocationId;
    const now = new Date();
    const from = new Date(now.getTime() - 72 * 3600 * 1000);
    const prefix = locId ? `${locId}:` : "";
    try {
      const pts = await AppService.GetTimeSeries(prefix + formatKey(from), prefix + formatKey(now));
      series = pts ?? [];
      renderChart();
    } catch {
      // Ignore transient load errors
    }
  }

  function renderChart() {
    if (!chart) return;

    if (mode === "compare" && appState.weathers.length > 0) {
      const locs = appState.config?.locations ?? [];
      const items = appState.weathers.map((w) => {
        const loc = locs.find((l) => l.id === w.locationId);
        return {
          name: loc ? locDisplayName(loc) : w.locationName,
          temp: Number(w.temperature2m.toFixed(1)),
          pressure: Number(w.pressureMsl.toFixed(1)),
          cape: Math.round(w.cape),
        };
      });

      chart.setOption(
        {
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: {
            type: "scroll",
            data: [t("metrics.temperature"), t("metrics.pressureMsl"), t("metrics.cape")],
            textStyle: { color: "#94a3b8", fontSize: 10 },
            top: 2,
          },
          grid: { left: 40, right: 44, top: 38, bottom: 26 },
          xAxis: {
            type: "category",
            data: items.map((i) => i.name),
            axisLabel: { color: "#94a3b8", fontSize: 10 },
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

    const labels = series.map(
      (p) => `${p.timestamp.slice(4, 6)}/${p.timestamp.slice(6, 8)} ${p.timestamp.slice(8, 10)}:${p.timestamp.slice(10, 12)}`
    );
    chart.setOption(
      {
        backgroundColor: "transparent",
        tooltip: { trigger: "axis" },
        legend: {
          type: "scroll",
          data: [t("metrics.pressureMsl"), t("metrics.temperature"), t("metrics.cape"), t("metrics.kpIndex")],
          textStyle: { color: "#94a3b8", fontSize: 10 },
          top: 2,
        },
        grid: { left: 42, right: 42, top: 38, bottom: 26 },
        xAxis: {
          type: "category",
          data: labels,
          axisLabel: { color: "#64748b", fontSize: 10 },
        },
        yAxis: [
          {
            type: "value",
            name: "hPa",
            scale: true,
            axisLabel: { color: "#38bdf8", fontSize: 10 },
            splitLine: { lineStyle: { color: "rgba(148,163,184,0.1)" } },
          },
          {
            type: "value",
            name: "°C / CAPE",
            axisLabel: { color: "#fbbf24", fontSize: 10 },
            splitLine: { show: false },
          },
        ],
        series: [
          {
            name: t("metrics.pressureMsl"),
            type: "line",
            smooth: true,
            data: series.map((p) => p.pressureMsl),
            lineStyle: { color: "#38bdf8", width: 2 },
            itemStyle: { color: "#38bdf8" },
          },
          {
            name: t("metrics.temperature"),
            type: "line",
            yAxisIndex: 1,
            smooth: true,
            data: series.map((p) => p.temperature2m),
            lineStyle: { color: "#fbbf24", width: 2 },
            itemStyle: { color: "#fbbf24" },
          },
          {
            name: t("metrics.cape"),
            type: "bar",
            yAxisIndex: 1,
            data: series.map((p) => p.cape),
            itemStyle: { color: "rgba(250, 204, 21, 0.35)" },
          },
          {
            name: t("metrics.kpIndex"),
            type: "line",
            yAxisIndex: 1,
            smooth: true,
            data: series.map((p) => p.kpIndex),
            lineStyle: { color: "#c084fc", width: 1.5 },
            itemStyle: { color: "#c084fc" },
          },
        ],
      },
      true
    );
  }

  onMount(() => {
    chart = echarts.init(chartEl);
    void loadSeries();
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
    void mode;
    void t("app.title");
    if (mode === "trend") {
      void loadSeries();
    } else {
      renderChart();
    }
  });
</script>

<div class="charts-panel">
  <div class="panel-header">
    <div class="title-row">
      <h3>{mode === "compare" ? t("charts.multiCityCompare") : t("charts.historyTrend")}</h3>
      <div class="mode-switch">
        <button class:active={mode === "compare"} onclick={() => (mode = "compare")}>
          {t("location.allLocations")}
        </button>
        <button class:active={mode === "trend"} onclick={() => (mode = "trend")}>
          {t("location.singleLocation")}
        </button>
      </div>
    </div>
  </div>

  <div class="chart-box" bind:this={chartEl}></div>

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

  {#if appState.weathers.length > 1}
    <div class="city-table">
      {#each appState.weathers as w (w.locationId)}
        {@const loc = appState.config?.locations?.find((l) => l.id === w.locationId)}
        <button
          class="city-row"
          class:selected={w.locationId === appState.selectedLocationId}
          onclick={() => selectLocation(w.locationId, true)}
        >
          <span class="c-name">{loc ? locDisplayName(loc) : w.locationName}</span>
          <span>{w.temperature2m.toFixed(1)}°C</span>
          <span>{w.pressureMsl.toFixed(1)} hPa</span>
          <span>{w.relativeHumidity2m.toFixed(0)}%</span>
        </button>
      {/each}
    </div>
  {/if}
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
  .title-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  h3 {
    margin: 0;
    font-size: 0.9rem;
    color: #e2e8f0;
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
    min-height: 210px;
    flex: 1 0 210px;
  }
  .kpi-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
    gap: 6px;
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
  .city-row {
    display: grid;
    grid-template-columns: 1.4fr 1fr 1.2fr 0.7fr;
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
    font-weight: 600;
    color: #f8fafc;
  }
</style>
