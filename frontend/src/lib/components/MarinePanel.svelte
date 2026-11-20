<script lang="ts">
  import { onMount } from "svelte";
  import * as echarts from "echarts";
  import { appState, selectMarineLocation } from "../stores/appState.svelte";
  import { locDisplayName, t } from "../i18n/index.svelte";

  let chartEl: HTMLDivElement;
  let chart: echarts.ECharts | null = null;

  function renderChart() {
    if (!chart) return;
    const locs = appState.config?.marineLocations ?? [];
    const items = appState.marines.map((m) => {
      const loc = locs.find((l) => l.id === m.locationId);
      return {
        name: loc ? locDisplayName(loc) : m.locationName,
        sst: Number(m.seaSurfaceTemperature.toFixed(1)),
        wave: Number(m.waveHeight.toFixed(2)),
        current: Number(m.oceanCurrentVelocity.toFixed(2)),
      };
    });

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
  }

  onMount(() => {
    chart = echarts.init(chartEl);
    renderChart();
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
    void t("app.title");
    renderChart();
  });
</script>

<div class="marine-panel">
  <div class="head">
    <h3>{t("charts.marineCompare")}</h3>
  </div>

  <div class="chart-box" bind:this={chartEl}></div>

  <div class="marine-list">
    {#each appState.config?.marineLocations ?? [] as ml (ml.id)}
      {@const snap = appState.marines.find((m) => m.locationId === ml.id)}
      {@const selected = ml.id === appState.selectedMarineId}
      <button class="marine-card" class:selected onclick={() => selectMarineLocation(ml.id, true)}>
        <div class="m-top">
          <span class="m-title">{locDisplayName(ml)}</span>
          <span class="m-coord">{ml.latitude.toFixed(2)}, {ml.longitude.toFixed(2)}</span>
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
    gap: 12px;
    padding: 12px;
    height: 100%;
    box-sizing: border-box;
    overflow-y: auto;
  }
  h3 {
    margin: 0;
    font-size: 0.92rem;
    color: #e2e8f0;
  }
  .chart-box {
    width: 100%;
    min-height: 220px;
    flex: 0 0 220px;
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
