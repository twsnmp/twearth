<script lang="ts">
  import { onMount } from "svelte";
  import * as echarts from "echarts";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type { TimeSeriesPoint } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import { appState } from "../stores/appState.svelte";

  let chartEl: HTMLDivElement;
  let chart: echarts.ECharts | null = null;
  let series = $state<TimeSeriesPoint[]>([]);

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
    const now = new Date();
    const from = new Date(now.getTime() - 72 * 3600 * 1000);
    try {
      const pts = await AppService.GetTimeSeries(formatKey(from), formatKey(now));
      series = pts ?? [];
      renderChart();
    } catch {
      // Ignore transient load errors
    }
  }

  function renderChart() {
    if (!chart) return;
    const labels = series.map((p) => `${p.timestamp.slice(4, 6)}/${p.timestamp.slice(6, 8)} ${p.timestamp.slice(8, 10)}:${p.timestamp.slice(10, 12)}`);
    chart.setOption({
      backgroundColor: "transparent",
      tooltip: { trigger: "axis" },
      legend: {
        type: "scroll",
        data: ["気圧(hPa)", "CAPE(J/kg)", "SST(°C)", "Kp指数"],
        textStyle: { color: "#94a3b8", fontSize: 10 },
        top: 2,
      },
      grid: { left: 42, right: 42, top: 44, bottom: 26 },
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
          name: "CAPE/Kp",
          axisLabel: { color: "#facc15", fontSize: 10 },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: "気圧(hPa)",
          type: "line",
          smooth: true,
          data: series.map((p) => p.pressureMsl),
          lineStyle: { color: "#38bdf8", width: 2 },
          itemStyle: { color: "#38bdf8" },
        },
        {
          name: "CAPE(J/kg)",
          type: "bar",
          yAxisIndex: 1,
          data: series.map((p) => p.cape),
          itemStyle: { color: "rgba(250, 204, 21, 0.55)" },
        },
        {
          name: "SST(°C)",
          type: "line",
          yAxisIndex: 1,
          smooth: true,
          data: series.map((p) => p.seaSurfaceTemp),
          lineStyle: { color: "#4ade80", width: 1.5 },
          itemStyle: { color: "#4ade80" },
        },
        {
          name: "Kp指数",
          type: "line",
          yAxisIndex: 1,
          data: series.map((p) => p.kpIndex),
          lineStyle: { color: "#f472b6", width: 1.5 },
          itemStyle: { color: "#f472b6" },
        },
      ],
    });
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
    if (appState.weather || appState.space || appState.marine) {
      void loadSeries();
    }
  });
</script>

<div class="charts-wrap">
  <div class="kpi-row">
    <div class="kpi">
      <span class="kpi-label">海面気圧</span>
      <span class="kpi-val">{appState.weather ? `${appState.weather.pressureMsl.toFixed(1)} hPa` : "--"}</span>
    </div>
    <div class="kpi">
      <span class="kpi-label">近海 SST</span>
      <span class="kpi-val">{appState.marine ? `${appState.marine.seaSurfaceTemperature.toFixed(1)} °C` : "--"}</span>
    </div>
    <div class="kpi">
      <span class="kpi-label">Kp / Flare</span>
      <span class="kpi-val">
        {appState.space ? `${appState.space.kpIndex.toFixed(1)} / ${appState.space.flareClass}` : "--"}
      </span>
    </div>
  </div>
  <div class="chart-box" bind:this={chartEl}></div>
</div>

<style>
  .charts-wrap {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 240px;
    padding: 8px;
    gap: 8px;
  }
  .kpi-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 6px;
  }
  .kpi {
    background: #070b17;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 6px;
    padding: 6px 8px;
    display: flex;
    flex-direction: column;
  }
  .kpi-label {
    font-size: 0.68rem;
    color: #94a3b8;
  }
  .kpi-val {
    font-size: 0.85rem;
    font-weight: 700;
    color: #38bdf8;
  }
  .chart-box {
    flex: 1;
    min-height: 180px;
    width: 100%;
  }
</style>
