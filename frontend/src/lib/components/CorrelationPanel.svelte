<script lang="ts">
  import { onMount } from "svelte";
  import * as echarts from "echarts";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type {
    CatalogLocation,
    CatalogMetric,
    CorrelationResult,
    SeriesCatalog,
  } from "../../../bindings/github.com/twsnmp/twearth/internal/analysis/models";
  import { appState, openViewAI } from "../stores/appState.svelte";
  import { locDisplayName, t } from "../i18n/index.svelte";

  type AlgId = "pearson" | "spearman" | "cross_correlation" | "tensai_mlp" | "tensai_lstm" | "tensai_knn";
  type ResultTab = "timeseries" | "scatter" | "lag" | "ml" | "confusion";

  let catalog = $state<SeriesCatalog>({ locations: [], metrics: [] });
  let loading = $state(false);
  let errorMsg = $state("");
  let result = $state<CorrelationResult | null>(null);
  let activeTab = $state<ResultTab>("timeseries");

  // Series X
  let xKind = $state<"weather" | "marine">("marine");
  let xLocationId = $state("");
  let xMetric = $state("seaSurfaceTemperature");

  // Series Y
  let yKind = $state<"weather" | "marine">("weather");
  let yLocationId = $state("");
  let yMetric = $state("temperature2m");

  // Conditions & Preprocessing
  let rangeKey = $state("5y");
  let granularity = $state("monthly");
  let deseasonalize = $state(true);
  let movingAvgWindow = $state(0);
  let difference = $state(false);
  let standardize = $state(false);
  let lag = $state(0);

  // Algorithm & Params
  let algorithm = $state<AlgId>("cross_correlation");
  let maxLag = $state(12);
  let lagWindow = $state(4);
  let hiddenSize = $state(16);
  let epochs = $state(200);
  let learningRate = $state(0.01);
  let activation = $state("tanh");
  let trainRatio = $state(0.8);
  let knnK = $state(5);
  let knnClasses = $state(3);

  let chartEl = $state<HTMLDivElement | null>(null);
  let chart: echarts.ECharts | null = null;

  const allLocations = $derived(catalog.locations ?? []);
  const allMetrics = $derived(catalog.metrics ?? []);
  const xLocations = $derived(allLocations.filter((l) => l.kind === xKind));
  const yLocations = $derived(allLocations.filter((l) => l.kind === yKind));
  const xMetrics = $derived(allMetrics.filter((m) => m.kind === xKind));
  const yMetrics = $derived(allMetrics.filter((m) => m.kind === yKind));

  const isML = $derived(algorithm === "tensai_mlp" || algorithm === "tensai_lstm" || algorithm === "tensai_knn");
  const isNN = $derived(algorithm === "tensai_mlp" || algorithm === "tensai_lstm");

  function displayLocName(loc: CatalogLocation): string {
    return locDisplayName(loc);
  }

  function displayMetricName(m: CatalogMetric): string {
    const key = `correlation.metrics.${m.id}`;
    const translated = t(key);
    return translated === key ? m.id : translated;
  }

  function syncDefaultsAfterCatalog() {
    if (!xLocationId && xLocations.length > 0) {
      const nino = xLocations.find((l) => l.id === "nino-3") ?? xLocations.find((l) => l.id === "nino-34") ?? xLocations[0];
      xLocationId = nino.id;
    }
    if (!yLocationId && yLocations.length > 0) {
      const primaryId = appState.config?.locations?.find((l) => l.isPrimary)?.id;
      const loc = yLocations.find((l) => l.id === primaryId) ?? yLocations[0];
      yLocationId = loc.id;
    }
  }

  function applyPreset(preset: "nino-temp" | "nino-japan-sst" | "nino34-temp") {
    const marineLocs = allLocations.filter((l) => l.kind === "marine");
    const weatherLocs = allLocations.filter((l) => l.kind === "weather");
    const primaryWeather = weatherLocs.find((l) => l.id === appState.selectedLocationId) ?? weatherLocs[0];

    if (preset === "nino-temp") {
      xKind = "marine";
      xLocationId = (marineLocs.find((l) => l.id === "nino-3") ?? marineLocs[0])?.id ?? "";
      xMetric = "seaSurfaceTemperature";
      yKind = "weather";
      yLocationId = primaryWeather?.id ?? "";
      yMetric = "temperature2m";
      rangeKey = "10y";
      granularity = "monthly";
      deseasonalize = true;
      algorithm = "cross_correlation";
      maxLag = 12;
    } else if (preset === "nino-japan-sst") {
      xKind = "marine";
      xLocationId = (marineLocs.find((l) => l.id === "nino-3") ?? marineLocs[0])?.id ?? "";
      xMetric = "seaSurfaceTemperature";
      yKind = "marine";
      const jpMarine =
        marineLocs.find((l) => l.id === "kuroshio-shionomisaki") ??
        marineLocs.find((l) => l.id === "tokyo-bay") ??
        marineLocs.find((l) => l.category === "regional") ??
        marineLocs[0];
      yLocationId = jpMarine?.id ?? "";
      yMetric = "seaSurfaceTemperature";
      rangeKey = "10y";
      granularity = "monthly";
      deseasonalize = true;
      algorithm = "cross_correlation";
      maxLag = 12;
    } else {
      xKind = "marine";
      xLocationId = (marineLocs.find((l) => l.id === "nino-34") ?? marineLocs.find((l) => l.id === "nino-3") ?? marineLocs[0])?.id ?? "";
      xMetric = "seaSurfaceTemperature";
      yKind = "weather";
      yLocationId = primaryWeather?.id ?? "";
      yMetric = "temperature2m";
      rangeKey = "10y";
      granularity = "monthly";
      deseasonalize = true;
      algorithm = "tensai_mlp";
      lagWindow = 6;
    }
  }

  function onChangeKind(side: "x" | "y", kind: "weather" | "marine") {
    if (side === "x") {
      xKind = kind;
      const locs = allLocations.filter((l) => l.kind === kind);
      xLocationId = locs[0]?.id ?? "";
      xMetric = kind === "marine" ? "seaSurfaceTemperature" : "temperature2m";
    } else {
      yKind = kind;
      const locs = allLocations.filter((l) => l.kind === kind);
      yLocationId = locs[0]?.id ?? "";
      yMetric = kind === "marine" ? "seaSurfaceTemperature" : "temperature2m";
    }
  }

  async function runAnalysis() {
    if (!xLocationId || !yLocationId) return;
    loading = true;
    errorMsg = "";
    try {
      const res = await AppService.AnalyzeCorrelation({
        seriesX: { kind: xKind, locationId: xLocationId, metric: xMetric },
        seriesY: { kind: yKind, locationId: yLocationId, metric: yMetric },
        rangeKey,
        granularity,
        preprocess: {
          deseasonalize,
          movingAvgWindow,
          difference,
          standardize,
          lag,
        },
        algorithm,
        params: {
          maxLag,
          lagWindow,
          hiddenSize,
          epochs,
          learningRate,
          activation,
          trainRatio,
          k: knnK,
          classes: knnClasses,
        },
      });
      result = res;

      if (res.algorithm === "cross_correlation") {
        activeTab = "lag";
      } else if (res.algorithm === "tensai_knn") {
        activeTab = "confusion";
      } else if (res.algorithm === "tensai_mlp" || res.algorithm === "tensai_lstm") {
        activeTab = "timeseries";
      } else {
        activeTab = "scatter";
      }
      queueMicrotask(renderChart);
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function renderChart() {
    if (!chartEl || !result) return;
    if (!chart) {
      chart = echarts.init(chartEl);
    }
    chart.clear();

    const r = result;
    const xLoc = allLocations.find((l) => l.id === xLocationId);
    const yLoc = allLocations.find((l) => l.id === yLocationId);
    const xName = `${xLoc ? displayLocName(xLoc) : xLocationId} (${xMetric})`;
    const yName = `${yLoc ? displayLocName(yLoc) : yLocationId} (${yMetric})`;

    if (activeTab === "timeseries") {
      const ts = r.timeSeries ?? [];
      const labels = ts.map((p) => p.label);
      const xVals = ts.map((p) => Number(p.x.toFixed(4)));
      const yVals = ts.map((p) => Number(p.y.toFixed(4)));
      const predVals = ts.map((p) => (p.predictedY != null ? Number(p.predictedY.toFixed(4)) : null));
      const hasPred = predVals.some((v) => v != null) && r.algorithm !== "tensai_knn";

      const testStartIdx = ts.findIndex((p) => p.isTest);
      const markAreaData =
        testStartIdx >= 0
          ? [[{ xAxis: labels[testStartIdx] }, { xAxis: labels[labels.length - 1] }]]
          : [];

      const series: echarts.SeriesOption[] = hasPred
        ? [
            {
              name: t("correlation.actualY"),
              type: "line",
              data: yVals,
              smooth: false,
              showSymbol: false,
              lineStyle: { width: 2, color: "#38bdf8" },
              markArea: markAreaData.length
                ? {
                    itemStyle: { color: "rgba(34, 197, 94, 0.08)" },
                    label: { show: true, position: "insideTop", formatter: "Test", color: "#86efac", fontSize: 11 },
                    data: markAreaData as any,
                  }
                : undefined,
            },
            {
              name: t("correlation.predictedY"),
              type: "line",
              data: predVals,
              smooth: false,
              showSymbol: false,
              lineStyle: { width: 2, type: "dashed", color: "#f59e0b" },
            },
          ]
        : [
            {
              name: xName,
              type: "line",
              yAxisIndex: 0,
              data: xVals,
              showSymbol: false,
              lineStyle: { width: 2, color: "#f59e0b" },
            },
            {
              name: yName,
              type: "line",
              yAxisIndex: 1,
              data: yVals,
              showSymbol: false,
              lineStyle: { width: 2, color: "#38bdf8" },
            },
          ];

      chart.setOption({
        backgroundColor: "transparent",
        tooltip: { trigger: "axis" },
        legend: { top: 4, textStyle: { color: "#cbd5e1", fontSize: 11 } },
        grid: { left: 56, right: hasPred ? 24 : 56, top: 40, bottom: 48 },
        dataZoom: [{ type: "inside" }, { type: "slider", height: 18, bottom: 8 }],
        xAxis: {
          type: "category",
          data: labels,
          axisLabel: { color: "#94a3b8", fontSize: 10 },
          axisLine: { lineStyle: { color: "#334155" } },
        },
        yAxis: hasPred
          ? [{ type: "value", axisLabel: { color: "#94a3b8", fontSize: 10 }, splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } } }]
          : [
              { type: "value", name: "X", nameTextStyle: { color: "#f59e0b" }, axisLabel: { color: "#f59e0b", fontSize: 10 }, splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } } },
              { type: "value", name: "Y", nameTextStyle: { color: "#38bdf8" }, axisLabel: { color: "#38bdf8", fontSize: 10 }, splitLine: { show: false } },
            ],
        series,
      });
      return;
    }

    if (activeTab === "scatter") {
      const pts = r.scatter ?? [];
      const regLine = (r.regressionLine ?? []).map((p) => [p.x, p.y]);
      const series: echarts.SeriesOption[] = [
        {
          name: isNN ? "Actual vs Predicted" : "Samples",
          type: "scatter",
          symbolSize: 7,
          itemStyle: { color: "rgba(56, 189, 248, 0.78)" },
          data: pts.map((p) => [Number(p.x.toFixed(4)), Number(p.y.toFixed(4)), p.label ?? ""]),
        },
      ];
      if (regLine.length === 2) {
        series.push({
          name: "Linear Fit",
          type: "line",
          showSymbol: false,
          lineStyle: { color: "#f59e0b", width: 2 },
          data: regLine,
        });
      }
      chart.setOption({
        backgroundColor: "transparent",
        tooltip: {
          trigger: "item",
          formatter: (p: any) => {
            const d = p.data;
            if (!Array.isArray(d)) return "";
            const lbl = d[2] ? `${d[2]}<br/>` : "";
            return `${lbl}X: ${d[0]}<br/>Y: ${d[1]}`;
          },
        },
        grid: { left: 56, right: 24, top: 32, bottom: 44 },
        xAxis: {
          type: "value",
          name: isNN ? t("correlation.actualY") : "X",
          nameLocation: "middle",
          nameGap: 26,
          nameTextStyle: { color: "#94a3b8" },
          axisLabel: { color: "#94a3b8", fontSize: 10 },
          splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } },
        },
        yAxis: {
          type: "value",
          name: isNN ? t("correlation.predictedY") : "Y",
          nameTextStyle: { color: "#94a3b8" },
          axisLabel: { color: "#94a3b8", fontSize: 10 },
          splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } },
        },
        series,
      });
      return;
    }

    if (activeTab === "lag") {
      const prof = r.lagProfile ?? [];
      chart.setOption({
        backgroundColor: "transparent",
        tooltip: { trigger: "axis" },
        grid: { left: 56, right: 24, top: 32, bottom: 44 },
        xAxis: {
          type: "category",
          data: prof.map((p) => String(p.lag)),
          name: "Lag",
          nameLocation: "middle",
          nameGap: 26,
          nameTextStyle: { color: "#94a3b8" },
          axisLabel: { color: "#94a3b8", fontSize: 10 },
        },
        yAxis: {
          type: "value",
          min: -1,
          max: 1,
          axisLabel: { color: "#94a3b8", fontSize: 10 },
          splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } },
        },
        series: [
          {
            name: "r",
            type: "bar",
            data: prof.map((p) => ({
              value: Number(p.correlation.toFixed(4)),
              itemStyle: {
                color: p.lag === r.metrics.bestLag ? "#f59e0b" : p.correlation >= 0 ? "#38bdf8" : "#fb7185",
              },
            })),
          },
        ],
      });
      return;
    }

    if (activeTab === "ml") {
      const losses = r.lossHistory ?? [];
      const imps = r.featureImportances ?? [];
      if (imps.length > 0) {
        chart.setOption({
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: { top: 4, textStyle: { color: "#cbd5e1", fontSize: 11 } },
          grid: [
            { left: 56, right: "54%", top: 40, bottom: 40 },
            { left: "54%", right: 24, top: 40, bottom: 40 },
          ],
          xAxis: [
            { gridIndex: 0, type: "category", data: losses.map((l) => String(l.epoch)), axisLabel: { color: "#94a3b8", fontSize: 10 } },
            { gridIndex: 1, type: "value", axisLabel: { color: "#94a3b8", fontSize: 10 }, splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } } },
          ],
          yAxis: [
            { gridIndex: 0, type: "value", axisLabel: { color: "#94a3b8", fontSize: 10 }, splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } } },
            { gridIndex: 1, type: "category", data: imps.map((f) => f.name), axisLabel: { color: "#94a3b8", fontSize: 10 } },
          ],
          series: [
            { name: t("correlation.trainLoss"), type: "line", xAxisIndex: 0, yAxisIndex: 0, showSymbol: false, data: losses.map((l) => Number(l.trainLoss.toFixed(4))), lineStyle: { color: "#38bdf8" } },
            { name: t("correlation.testLoss"), type: "line", xAxisIndex: 0, yAxisIndex: 0, showSymbol: false, data: losses.map((l) => Number(l.testLoss.toFixed(4))), lineStyle: { color: "#f59e0b" } },
            { name: t("correlation.importance"), type: "bar", xAxisIndex: 1, yAxisIndex: 1, data: imps.map((f) => Number((f.importance * 100).toFixed(1))), itemStyle: { color: "#22c55e" } },
          ],
        });
      } else {
        chart.setOption({
          backgroundColor: "transparent",
          tooltip: { trigger: "axis" },
          legend: { top: 4, textStyle: { color: "#cbd5e1", fontSize: 11 } },
          grid: { left: 56, right: 24, top: 40, bottom: 40 },
          xAxis: { type: "category", data: losses.map((l) => String(l.epoch)), axisLabel: { color: "#94a3b8", fontSize: 10 } },
          yAxis: { type: "value", axisLabel: { color: "#94a3b8", fontSize: 10 }, splitLine: { lineStyle: { color: "rgba(148,163,184,0.12)" } } },
          series: [
            { name: t("correlation.trainLoss"), type: "line", showSymbol: false, data: losses.map((l) => Number(l.trainLoss.toFixed(4))), lineStyle: { color: "#38bdf8" } },
            { name: t("correlation.testLoss"), type: "line", showSymbol: false, data: losses.map((l) => Number(l.testLoss.toFixed(4))), lineStyle: { color: "#f59e0b" } },
          ],
        });
      }
      return;
    }

    if (activeTab === "confusion" && r.confusionMatrix) {
      const cm = r.confusionMatrix;
      const mat = cm.matrix ?? [];
      const heatData: [number, number, number][] = [];
      let maxVal = 1;
      for (let actual = 0; actual < mat.length; actual++) {
        const row = mat[actual] ?? [];
        for (let pred = 0; pred < row.length; pred++) {
          const v = row[pred];
          if (v > maxVal) maxVal = v;
          heatData.push([pred, actual, v]);
        }
      }
      chart.setOption({
        backgroundColor: "transparent",
        tooltip: { position: "top" },
        grid: { left: 80, right: 40, top: 32, bottom: 56 },
        xAxis: { type: "category", data: cm.labels ?? [], name: "Predicted", nameLocation: "middle", nameGap: 28, axisLabel: { color: "#cbd5e1" } },
        yAxis: { type: "category", data: cm.labels ?? [], name: "Actual", axisLabel: { color: "#cbd5e1" } },
        visualMap: { min: 0, max: maxVal, calculable: false, orient: "horizontal", left: "center", bottom: 0, inRange: { color: ["#0f172a", "#0284c7", "#38bdf8"] }, textStyle: { color: "#94a3b8" } },
        series: [{ name: "Count", type: "heatmap", data: heatData, label: { show: true, color: "#f8fafc", fontSize: 13 } }],
      });
    }
  }

  function explainWithAI() {
    if (!result) return;
    openViewAI({
      view: yKind === "marine" ? "marine" : "charts",
      mode: "trend",
      locationId: yKind === "weather" ? yLocationId : xKind === "weather" ? xLocationId : appState.selectedLocationId,
      marineId: xKind === "marine" ? xLocationId : yKind === "marine" ? yLocationId : "",
      metric: yMetric,
      rangeKey,
      granularity: result.granularity,
    });
  }

  $effect(() => {
    // Re-render when switching tabs
    if (activeTab && result) {
      queueMicrotask(renderChart);
    }
  });

  onMount(() => {
    let disposed = false;
    (async () => {
      try {
        catalog = await AppService.GetCorrelationSeriesCatalog();
        if (!disposed) {
          syncDefaultsAfterCatalog();
        }
      } catch (e) {
        errorMsg = e instanceof Error ? e.message : String(e);
      }
    })();

    const onResize = () => chart?.resize();
    window.addEventListener("resize", onResize);
    return () => {
      disposed = true;
      window.removeEventListener("resize", onResize);
      chart?.dispose();
      chart = null;
    };
  });
</script>

<div class="corr-root">
  <aside class="corr-sidebar">
    <div class="sidebar-header">
      <h2>{t("correlation.title")}</h2>
    </div>

    <div class="section">
      <div class="sec-title">{t("correlation.presets")}</div>
      <div class="preset-row">
        <button class="chip-btn" onclick={() => applyPreset("nino-temp")}>{t("correlation.presetNinoTemp")}</button>
        <button class="chip-btn" onclick={() => applyPreset("nino-japan-sst")}>{t("correlation.presetNinoJapanSST")}</button>
        <button class="chip-btn" onclick={() => applyPreset("nino34-temp")}>{t("correlation.presetNino34Temp")}</button>
      </div>
    </div>

    <div class="section">
      <div class="sec-title">{t("correlation.seriesX")}</div>
      <div class="row-2">
        <select value={xKind} onchange={(e) => onChangeKind("x", (e.currentTarget.value as "weather" | "marine"))}>
          <option value="marine">{t("correlation.kindMarine")}</option>
          <option value="weather">{t("correlation.kindWeather")}</option>
        </select>
        <select bind:value={xMetric}>
          {#each xMetrics as m}
            <option value={m.id}>{displayMetricName(m)}</option>
          {/each}
        </select>
      </div>
      <select bind:value={xLocationId}>
        {#each xLocations as loc}
          <option value={loc.id}>{displayLocName(loc)}</option>
        {/each}
      </select>
    </div>

    <div class="section">
      <div class="sec-title">{t("correlation.seriesY")}</div>
      <div class="row-2">
        <select value={yKind} onchange={(e) => onChangeKind("y", (e.currentTarget.value as "weather" | "marine"))}>
          <option value="weather">{t("correlation.kindWeather")}</option>
          <option value="marine">{t("correlation.kindMarine")}</option>
        </select>
        <select bind:value={yMetric}>
          {#each yMetrics as m}
            <option value={m.id}>{displayMetricName(m)}</option>
          {/each}
        </select>
      </div>
      <select bind:value={yLocationId}>
        {#each yLocations as loc}
          <option value={loc.id}>{displayLocName(loc)}</option>
        {/each}
      </select>
    </div>

    <div class="section">
      <div class="sec-title">{t("correlation.preprocess")}</div>
      <div class="row-2">
        <label class="field">
          <span>{t("correlation.range")}</span>
          <select bind:value={rangeKey}>
            <option value="30d">30d</option>
            <option value="90d">90d</option>
            <option value="1y">1y</option>
            <option value="3y">3y</option>
            <option value="5y">5y</option>
            <option value="10y">10y</option>
            <option value="30y">30y</option>
            <option value="all">All</option>
          </select>
        </label>
        <label class="field">
          <span>{t("correlation.granularity")}</span>
          <select bind:value={granularity}>
            <option value="daily">{t("correlation.granDaily")}</option>
            <option value="monthly">{t("correlation.granMonthly")}</option>
            <option value="hourly">{t("correlation.granHourly")}</option>
          </select>
        </label>
      </div>

      <label class="check-row" title={t("correlation.deseasonalizeHint")}>
        <input type="checkbox" bind:checked={deseasonalize} />
        <span>{t("correlation.deseasonalize")}</span>
      </label>
      <div class="row-2">
        <label class="check-row">
          <input type="checkbox" bind:checked={difference} />
          <span>{t("correlation.difference")}</span>
        </label>
        <label class="check-row">
          <input type="checkbox" bind:checked={standardize} />
          <span>{t("correlation.standardize")}</span>
        </label>
      </div>

      <div class="row-2">
        <label class="field">
          <span>{t("correlation.movingAvg")}</span>
          <input type="number" min="0" max="90" bind:value={movingAvgWindow} />
        </label>
        <label class="field">
          <span>{t("correlation.lag")}</span>
          <input type="number" min="-60" max="60" bind:value={lag} disabled={algorithm === "cross_correlation"} />
        </label>
      </div>
    </div>

    <div class="section">
      <div class="sec-title">{t("correlation.algorithm")}</div>
      <select bind:value={algorithm}>
        <option value="pearson">{t("correlation.algPearson")}</option>
        <option value="spearman">{t("correlation.algSpearman")}</option>
        <option value="cross_correlation">{t("correlation.algCrossCorr")}</option>
        <option value="tensai_mlp">{t("correlation.algTensaiMLP")}</option>
        <option value="tensai_lstm">{t("correlation.algTensaiLSTM")}</option>
        <option value="tensai_knn">{t("correlation.algTensaiKNN")}</option>
      </select>

      {#if algorithm === "cross_correlation"}
        <label class="field">
          <span>{t("correlation.maxLag")}</span>
          <input type="number" min="1" max="120" bind:value={maxLag} />
        </label>
      {:else if isML}
        <div class="row-2">
          <label class="field">
            <span>{t("correlation.lagWindow")}</span>
            <input type="number" min="0" max="24" bind:value={lagWindow} />
          </label>
          <label class="field">
            <span>{t("correlation.trainRatio")}</span>
            <input type="number" min="0.5" max="0.9" step="0.05" bind:value={trainRatio} />
          </label>
        </div>
        {#if isNN}
          <div class="row-2">
            <label class="field">
              <span>{t("correlation.hiddenSize")}</span>
              <input type="number" min="4" max="128" bind:value={hiddenSize} />
            </label>
            <label class="field">
              <span>{t("correlation.epochs")}</span>
              <input type="number" min="20" max="1000" step="20" bind:value={epochs} />
            </label>
          </div>
          <div class="row-2">
            <label class="field">
              <span>{t("correlation.learningRate")}</span>
              <input type="number" min="0.0005" max="0.2" step="0.005" bind:value={learningRate} />
            </label>
            {#if algorithm === "tensai_mlp"}
              <label class="field">
                <span>{t("correlation.activation")}</span>
                <select bind:value={activation}>
                  <option value="tanh">Tanh</option>
                  <option value="relu">ReLU</option>
                  <option value="leaky_relu">LeakyReLU</option>
                </select>
              </label>
            {/if}
          </div>
        {:else if algorithm === "tensai_knn"}
          <div class="row-2">
            <label class="field">
              <span>{t("correlation.knnK")}</span>
              <input type="number" min="1" max="25" bind:value={knnK} />
            </label>
            <label class="field">
              <span>{t("correlation.knnClasses")}</span>
              <select bind:value={knnClasses}>
                <option value={3}>3 (Low/Normal/High)</option>
                <option value={2}>2 (Low/High)</option>
              </select>
            </label>
          </div>
        {/if}
      {/if}
    </div>

    <div class="actions">
      <button class="run-btn" disabled={loading || !xLocationId || !yLocationId} onclick={runAnalysis}>
        {loading ? t("correlation.running") : t("correlation.run")}
      </button>
      {#if result}
        <button class="ai-explain-btn" onclick={explainWithAI}>
          ✨ {t("correlation.askAI")}
        </button>
      {/if}
    </div>
  </aside>

  <main class="corr-main">
    {#if errorMsg}
      <div class="error-banner">{errorMsg}</div>
    {/if}

    {#if result}
      {#if result.warnings && result.warnings.length > 0}
        <div class="warn-banner">{result.warnings.join(" / ")}</div>
      {/if}

      <div class="kpi-grid">
        <div class="kpi-card">
          <div class="kpi-label">{t("correlation.samples")}</div>
          <div class="kpi-val">{result.sampleCount}</div>
        </div>

        {#if result.algorithm === "tensai_knn"}
          <div class="kpi-card">
            <div class="kpi-label">{t("correlation.accuracy")}</div>
            <div class="kpi-val">{(result.metrics.accuracy * 100).toFixed(1)}%</div>
          </div>
          <div class="kpi-card">
            <div class="kpi-label">{t("correlation.splitCount")}</div>
            <div class="kpi-val">{result.trainCount} / {result.testCount}</div>
          </div>
        {:else}
          <div class="kpi-card">
            <div class="kpi-label">{t("correlation.corrCoef")}</div>
            <div class="kpi-val">{result.metrics.correlation.toFixed(4)}</div>
          </div>
          <div class="kpi-card">
            <div class="kpi-label">{t("correlation.r2")}</div>
            <div class="kpi-val">{result.metrics.r2.toFixed(4)}</div>
          </div>
          <div class="kpi-card">
            <div class="kpi-label">{t("correlation.pValue")}</div>
            <div class="kpi-val">{result.metrics.pValue < 0.0001 ? result.metrics.pValue.toExponential(2) : result.metrics.pValue.toFixed(4)}</div>
          </div>
          {#if result.algorithm === "cross_correlation"}
            <div class="kpi-card highlight">
              <div class="kpi-label">{t("correlation.bestLag")}</div>
              <div class="kpi-val">{result.metrics.bestLag} ({result.metrics.bestLagCorrelation.toFixed(3)})</div>
            </div>
          {:else if isNN}
            <div class="kpi-card">
              <div class="kpi-label">{t("correlation.rmse")} / {t("correlation.mae")}</div>
              <div class="kpi-val">{result.metrics.rmse.toFixed(3)} / {result.metrics.mae.toFixed(3)}</div>
            </div>
          {/if}
        {/if}
      </div>

      <div class="tab-bar">
        <button class:active={activeTab === "timeseries"} onclick={() => (activeTab = "timeseries")}>{t("correlation.tabTimeseries")}</button>
        {#if result.scatter && result.scatter.length > 0}
          <button class:active={activeTab === "scatter"} onclick={() => (activeTab = "scatter")}>{t("correlation.tabScatter")}</button>
        {/if}
        {#if result.lagProfile && result.lagProfile.length > 0}
          <button class:active={activeTab === "lag"} onclick={() => (activeTab = "lag")}>{t("correlation.tabLag")}</button>
        {/if}
        {#if result.lossHistory && result.lossHistory.length > 0}
          <button class:active={activeTab === "ml"} onclick={() => (activeTab = "ml")}>{t("correlation.tabML")}</button>
        {/if}
        {#if result.confusionMatrix}
          <button class:active={activeTab === "confusion"} onclick={() => (activeTab = "confusion")}>{t("correlation.tabConfusion")}</button>
        {/if}
      </div>

      <div class="chart-wrap" bind:this={chartEl}></div>
    {:else}
      <div class="empty-state">
        <p>{t("correlation.noResult")}</p>
      </div>
    {/if}
  </main>
</div>

<style>
  .corr-root {
    display: grid;
    grid-template-columns: 330px 1fr;
    gap: 12px;
    height: 100%;
    min-height: 0;
    color: #e2e8f0;
  }

  .corr-sidebar {
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow-y: auto;
    padding: 12px;
    background: rgba(15, 23, 42, 0.72);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 10px;
  }

  .sidebar-header h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: #f8fafc;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid rgba(148, 163, 184, 0.12);
  }

  .sec-title {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #94a3b8;
  }

  .preset-row {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  .chip-btn {
    background: rgba(56, 189, 248, 0.12);
    color: #7dd3fc;
    border: 1px solid rgba(56, 189, 248, 0.28);
    border-radius: 6px;
    padding: 4px 8px;
    font-size: 11px;
    cursor: pointer;
  }
  .chip-btn:hover {
    background: rgba(56, 189, 248, 0.24);
  }

  .row-2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }

  select,
  input[type="number"] {
    width: 100%;
    box-sizing: border-box;
    background: #0f172a;
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 5px 7px;
    font-size: 12px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 3px;
    font-size: 11px;
    color: #94a3b8;
  }

  .check-row {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: #cbd5e1;
    cursor: pointer;
  }

  .actions {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 4px;
  }

  .run-btn {
    background: #0284c7;
    color: #f8fafc;
    border: none;
    border-radius: 8px;
    padding: 9px 12px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }
  .run-btn:hover:not(:disabled) {
    background: #0ea5e9;
  }
  .run-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .ai-explain-btn {
    background: rgba(168, 85, 247, 0.16);
    color: #e9d5ff;
    border: 1px solid rgba(168, 85, 247, 0.35);
    border-radius: 8px;
    padding: 7px 10px;
    font-size: 12px;
    cursor: pointer;
  }

  .corr-main {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-height: 0;
    padding: 12px;
    background: rgba(15, 23, 42, 0.72);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 10px;
  }

  .error-banner {
    background: rgba(239, 68, 68, 0.18);
    border: 1px solid rgba(239, 68, 68, 0.45);
    color: #fecaca;
    padding: 8px 12px;
    border-radius: 6px;
    font-size: 12px;
  }

  .warn-banner {
    background: rgba(245, 158, 11, 0.16);
    border: 1px solid rgba(245, 158, 11, 0.4);
    color: #fde68a;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
  }

  .kpi-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .kpi-card {
    flex: 1 1 130px;
    background: rgba(30, 41, 59, 0.65);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 8px;
    padding: 8px 10px;
  }
  .kpi-card.highlight {
    border-color: rgba(245, 158, 11, 0.5);
  }

  .kpi-label {
    font-size: 11px;
    color: #94a3b8;
  }

  .kpi-val {
    font-size: 16px;
    font-weight: 700;
    color: #f8fafc;
    margin-top: 2px;
  }

  .tab-bar {
    display: flex;
    gap: 6px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.16);
    padding-bottom: 6px;
  }

  .tab-bar button {
    background: transparent;
    color: #94a3b8;
    border: 1px solid transparent;
    border-radius: 6px;
    padding: 5px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .tab-bar button.active {
    background: rgba(56, 189, 248, 0.15);
    color: #7dd3fc;
    border-color: rgba(56, 189, 248, 0.35);
  }

  .chart-wrap {
    flex: 1 1 auto;
    min-height: 360px;
    width: 100%;
  }

  .empty-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #94a3b8;
    font-size: 13px;
  }
</style>
