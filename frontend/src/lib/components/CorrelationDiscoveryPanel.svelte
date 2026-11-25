<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type {
    CatalogKind,
    CatalogLocation,
    CatalogMetric,
    DiscoveryProgress,
    DiscoveryResultItem,
    SeriesCatalog,
  } from "../../../bindings/github.com/twsnmp/twearth/internal/analysis/models";
  import { locDisplayName, t } from "../i18n/index.svelte";

  type SortMode = "score" | "pvalue" | "samples" | "updated";

  let catalog = $state<SeriesCatalog>({ kinds: [], locations: [], metrics: [] });
  let progress = $state<DiscoveryProgress>({
    status: "idle",
    config: {
      rangeKey: "5y",
      granularity: "monthly",
      algorithms: ["cross_correlation"],
      deseasonalizeModes: [true],
      difference: false,
      movingAvgWindow: 0,
      maxLag: 12,
      minSamples: 12,
      sameLocationAllowed: false,
      crossKindOnly: false,
      forceRerun: false,
    },
    total: 0,
    completed: 0,
    cacheHits: 0,
    computed: 0,
    skipped: 0,
    errors: 0,
    updatedAt: "",
  });

  let items = $state<DiscoveryResultItem[]>([]);
  let selectedKeys = $state<Record<string, boolean>>({});
  let errorMsg = $state("");
  let confirmClear = $state(false);

  // Discovery scope controls
  let selectedKinds = $state<string[]>([]);
  let selectedAlgs = $state<string[]>(["cross_correlation"]);
  let deseasonOn = $state(true);
  let deseasonOff = $state(false);
  let rangeKey = $state("5y");
  let granularity = $state("monthly");
  let difference = $state(false);
  let movingAvgWindow = $state(0);
  let maxLag = $state(12);
  let minSamples = $state(12);
  let sameLocationAllowed = $state(false);
  let crossKindOnly = $state(false);
  let forceRerun = $state(false);

  // List filter & sort controls
  let searchText = $state("");
  let minScore = $state(0.4);
  let algFilter = $state("all");
  let excludeSameLocation = $state(true);
  let excludeIdentical = $state(true);
  let dedupePairs = $state(true);
  let sortBy = $state<SortMode>("score");

  const allKinds = $derived<CatalogKind[]>(
    catalog.kinds && catalog.kinds.length > 0
      ? catalog.kinds
      : [
          { id: "weather", defaultMetric: "temperature2m" },
          { id: "marine", defaultMetric: "seaSurfaceTemperature" },
        ]
  );

  const locMap = $derived.by(() => {
    const m = new Map<string, CatalogLocation>();
    for (const loc of catalog.locations ?? []) {
      m.set(`${loc.kind}:${loc.id}`, loc);
    }
    return m;
  });

  const metricMap = $derived.by(() => {
    const m = new Map<string, CatalogMetric>();
    for (const mt of catalog.metrics ?? []) {
      m.set(`${mt.kind}:${mt.id}`, mt);
    }
    return m;
  });

  const isRunning = $derived(progress.status === "running");
  const isPaused = $derived(progress.status === "paused");
  const progressPct = $derived(
    progress.total > 0 ? Math.min(100, Math.round((progress.completed / progress.total) * 100)) : 0
  );

  const checkedKeys = $derived(Object.keys(selectedKeys).filter((k) => selectedKeys[k]));

  const filteredItems = $derived.by(() => {
    const q = searchText.trim().toLowerCase();
    let list = items.filter((it) => {
      const sx = it.request.seriesX;
      const sy = it.request.seriesY;
      if (excludeSameLocation && sx.kind === sy.kind && sx.locationId === sy.locationId) {
        return false;
      }
      if (excludeIdentical && it.score >= 0.999) {
        return false;
      }
      if (it.score < minScore) return false;
      if (algFilter !== "all" && it.request.algorithm !== algFilter) return false;
      if (q) {
        const lx = formatSeriesLabel(sx.kind, sx.locationId, sx.metric).toLowerCase();
        const ly = formatSeriesLabel(sy.kind, sy.locationId, sy.metric).toLowerCase();
        if (!lx.includes(q) && !ly.includes(q)) return false;
      }
      return true;
    });

    if (dedupePairs) {
      const bestByPair = new Map<string, DiscoveryResultItem>();
      for (const it of list) {
        const a = `${it.request.seriesX.kind}:${it.request.seriesX.locationId}:${it.request.seriesX.metric}`;
        const b = `${it.request.seriesY.kind}:${it.request.seriesY.locationId}:${it.request.seriesY.metric}`;
        const pairKey = a < b ? `${a}|${b}` : `${b}|${a}`;
        const existing = bestByPair.get(pairKey);
        if (!existing || it.score > existing.score) {
          bestByPair.set(pairKey, it);
        }
      }
      list = Array.from(bestByPair.values());
    }

    list.sort((a, b) => {
      if (sortBy === "pvalue") {
        const pa = Number.isFinite(a.metrics.pValue) ? a.metrics.pValue : 1;
        const pb = Number.isFinite(b.metrics.pValue) ? b.metrics.pValue : 1;
        if (pa !== pb) return pa - pb;
        return b.score - a.score;
      }
      if (sortBy === "samples") {
        if (a.sampleCount !== b.sampleCount) return b.sampleCount - a.sampleCount;
        return b.score - a.score;
      }
      if (sortBy === "updated") {
        return (b.updatedAt ?? "").localeCompare(a.updatedAt ?? "");
      }
      if (b.score !== a.score) return b.score - a.score;
      return b.sampleCount - a.sampleCount;
    });
    return list;
  });

  function displayKindName(kindId: string): string {
    const key = `correlation.kinds.${kindId}`;
    const translated = t(key);
    return translated === key ? kindId : translated;
  }

  function displayMetricName(kind: string, metricId: string): string {
    const key = `correlation.metrics.${metricId}`;
    const translated = t(key);
    if (translated !== key) return translated;
    const mt = metricMap.get(`${kind}:${metricId}`);
    return mt ? `${mt.id} (${mt.unit})` : metricId;
  }

  function formatSeriesLabel(kind: string, locationId: string, metric: string): string {
    const loc = locMap.get(`${kind}:${locationId}`);
    const locName = loc ? locDisplayName(loc) : locationId;
    return `${locName} · ${displayMetricName(kind, metric)}`;
  }

  function toggleKind(kindId: string) {
    if (selectedKinds.includes(kindId)) {
      selectedKinds = selectedKinds.filter((k) => k !== kindId);
    } else {
      selectedKinds = [...selectedKinds, kindId];
    }
  }

  function toggleAlg(alg: string) {
    if (selectedAlgs.includes(alg)) {
      if (selectedAlgs.length > 1) {
        selectedAlgs = selectedAlgs.filter((a) => a !== alg);
      }
    } else {
      selectedAlgs = [...selectedAlgs, alg];
    }
  }

  function applyConfigFromProgress(prog: DiscoveryProgress) {
    if (!prog?.config) return;
    const c = prog.config;
    if (c.kinds && c.kinds.length > 0) selectedKinds = [...c.kinds];
    if (c.algorithms && c.algorithms.length > 0) selectedAlgs = [...c.algorithms];
    if (c.deseasonalizeModes && c.deseasonalizeModes.length > 0) {
      deseasonOn = c.deseasonalizeModes.includes(true);
      deseasonOff = c.deseasonalizeModes.includes(false);
    }
    if (c.rangeKey) rangeKey = c.rangeKey;
    if (c.granularity) granularity = c.granularity;
    difference = !!c.difference;
    movingAvgWindow = c.movingAvgWindow ?? 0;
    if (c.maxLag) maxLag = c.maxLag;
    if (c.minSamples) minSamples = c.minSamples;
    sameLocationAllowed = c.sameLocationAllowed ?? true;
    crossKindOnly = !!c.crossKindOnly;
    forceRerun = !!c.forceRerun;
  }

  async function startDiscovery() {
    errorMsg = "";
    const deseasonalizeModes: boolean[] = [];
    if (deseasonOn) deseasonalizeModes.push(true);
    if (deseasonOff) deseasonalizeModes.push(false);
    if (deseasonalizeModes.length === 0) deseasonalizeModes.push(true);

    try {
      await AppService.StartCorrelationDiscovery({
        kinds: selectedKinds,
        rangeKey,
        granularity,
        algorithms: selectedAlgs,
        deseasonalizeModes,
        difference,
        movingAvgWindow,
        maxLag,
        minSamples,
        sameLocationAllowed,
        crossKindOnly,
        forceRerun,
      });
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function pauseDiscovery() {
    errorMsg = "";
    try {
      await AppService.PauseCorrelationDiscovery();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function resumeDiscovery() {
    errorMsg = "";
    try {
      await AppService.ResumeCorrelationDiscovery();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function clearAllCache() {
    errorMsg = "";
    confirmClear = false;
    try {
      await AppService.ClearCorrelationCache();
      items = [];
      selectedKeys = {};
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function deleteKeys(keys: string[]) {
    if (keys.length === 0) return;
    errorMsg = "";
    try {
      await AppService.DeleteCorrelationCacheEntries(keys);
      const delSet = new Set(keys);
      items = items.filter((it) => !delSet.has(it.key));
      const nextSel = { ...selectedKeys };
      for (const k of keys) delete nextSel[k];
      selectedKeys = nextSel;
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function rerunKeys(keys: string[]) {
    if (keys.length === 0) return;
    errorMsg = "";
    try {
      await AppService.RerunCorrelationEntries(keys);
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function inspectItem(key: string) {
    errorMsg = "";
    try {
      await AppService.OpenCorrelationDetail(key, false);
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  function statusBadgeLabel(st: string): string {
    switch (st) {
      case "running":
        return t("discovery.statusRunning");
      case "paused":
        return t("discovery.statusPaused");
      case "completed":
        return t("discovery.statusCompleted");
      case "error":
        return t("discovery.statusError");
      default:
        return t("discovery.statusIdle");
    }
  }

  function algShortLabel(alg: string): string {
    switch (alg) {
      case "pearson":
        return "Pearson";
      case "spearman":
        return "Spearman";
      case "cross_correlation":
        return "Cross-Corr";
      default:
        return alg;
    }
  }

  function scoreClass(score: number): string {
    if (score >= 0.7) return "score-high";
    if (score >= 0.45) return "score-mid";
    return "score-low";
  }

  onMount(() => {
    let disposed = false;
    (async () => {
      try {
        const [cat, prog, cached] = await Promise.all([
          AppService.GetCorrelationSeriesCatalog(),
          AppService.GetCorrelationDiscoveryStatus(),
          AppService.ListCorrelationCache(),
        ]);
        if (disposed) return;
        catalog = cat;
        selectedKinds = (cat.kinds ?? []).map((k) => k.id);
        progress = prog;
        applyConfigFromProgress(prog);
        items = cached ?? [];
      } catch (e) {
        errorMsg = e instanceof Error ? e.message : String(e);
      }
    })();

    const offProg = Events.On("twearth:correlation-discovery-progress", (ev) => {
      const p = ev?.data as DiscoveryProgress | undefined;
      if (p) {
        progress = p;
      }
    });

    const offResult = Events.On("twearth:correlation-discovery-result", (ev) => {
      const item = ev?.data as DiscoveryResultItem | undefined;
      if (!item?.key) return;
      const idx = items.findIndex((x) => x.key === item.key);
      if (idx >= 0) {
        const next = [...items];
        next[idx] = item;
        items = next;
      } else {
        items = [item, ...items];
      }
    });

    return () => {
      disposed = true;
      offProg?.();
      offResult?.();
    };
  });
</script>

<div class="disc-root">
  <aside class="disc-sidebar">
    <div class="sidebar-header">
      <h2>{t("discovery.title")}</h2>
      <p class="subtitle">{t("discovery.subtitle")}</p>
    </div>

    <div class="section">
      <div class="sec-title">{t("discovery.scopeKinds")}</div>
      <div class="check-group">
        {#each allKinds as k}
          <label class="check-row">
            <input
              type="checkbox"
              checked={selectedKinds.length === 0 || selectedKinds.includes(k.id)}
              onchange={() => toggleKind(k.id)}
            />
            <span>{displayKindName(k.id)}</span>
          </label>
        {/each}
      </div>
      <label class="check-row">
        <input type="checkbox" bind:checked={crossKindOnly} />
        <span>{t("discovery.crossKindOnly")}</span>
      </label>
      <label class="check-row">
        <input type="checkbox" bind:checked={sameLocationAllowed} />
        <span>{t("discovery.sameLocationAllowed")}</span>
      </label>
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

      <div class="sec-sub">{t("discovery.scopeDeseasonalize")}</div>
      <div class="row-2">
        <label class="check-row">
          <input type="checkbox" bind:checked={deseasonOn} />
          <span>{t("discovery.deseasonOn")}</span>
        </label>
        <label class="check-row">
          <input type="checkbox" bind:checked={deseasonOff} />
          <span>{t("discovery.deseasonOff")}</span>
        </label>
      </div>

      <label class="check-row">
        <input type="checkbox" bind:checked={difference} />
        <span>{t("correlation.difference")}</span>
      </label>

      <div class="row-2">
        <label class="field">
          <span>{t("correlation.movingAvg")}</span>
          <input type="number" min="0" max="90" bind:value={movingAvgWindow} />
        </label>
        <label class="field">
          <span>{t("discovery.minSamples")}</span>
          <input type="number" min="3" max="1000" bind:value={minSamples} />
        </label>
      </div>
    </div>

    <div class="section">
      <div class="sec-title">{t("discovery.scopeAlgorithms")}</div>
      <label class="check-row">
        <input type="checkbox" checked={selectedAlgs.includes("pearson")} onchange={() => toggleAlg("pearson")} />
        <span>{t("correlation.algPearson")}</span>
      </label>
      <label class="check-row">
        <input type="checkbox" checked={selectedAlgs.includes("spearman")} onchange={() => toggleAlg("spearman")} />
        <span>{t("correlation.algSpearman")}</span>
      </label>
      <label class="check-row">
        <input
          type="checkbox"
          checked={selectedAlgs.includes("cross_correlation")}
          onchange={() => toggleAlg("cross_correlation")}
        />
        <span>{t("correlation.algCrossCorr")}</span>
      </label>

      {#if selectedAlgs.includes("cross_correlation")}
        <label class="field">
          <span>{t("correlation.maxLag")}</span>
          <input type="number" min="1" max="60" bind:value={maxLag} />
        </label>
      {/if}
    </div>

    <div class="section">
      <label class="check-row">
        <input type="checkbox" bind:checked={forceRerun} />
        <span>{t("discovery.forceRerun")}</span>
      </label>

      <div class="actions">
        {#if isRunning}
          <button class="pause-btn" onclick={pauseDiscovery}>{t("discovery.pause")}</button>
        {:else if isPaused}
          <button class="run-btn" onclick={resumeDiscovery}>{t("discovery.resume")}</button>
          <button class="secondary-btn" onclick={startDiscovery}>{t("discovery.start")}</button>
        {:else}
          <button class="run-btn" onclick={startDiscovery}>{t("discovery.start")}</button>
        {/if}
      </div>
    </div>
  </aside>

  <main class="disc-main">
    {#if errorMsg}
      <div class="error-banner">{errorMsg}</div>
    {/if}

    <section class="status-bar">
      <div class="status-top">
        <div class="status-left">
          <span class="status-badge status-{progress.status}">{statusBadgeLabel(progress.status)}</span>
          <span class="prog-text">
            {t("discovery.progressLabel")}: <strong>{progress.completed}</strong> / {progress.total} ({progressPct}%)
          </span>
          {#if progress.currentPair}
            <span class="current-pair" title={progress.currentPair}>
              [{algShortLabel(progress.currentAlgorithm ?? "")}] {progress.currentPair}
            </span>
          {/if}
        </div>
        <div class="status-metrics">
          <span>{t("discovery.computed")}: <strong>{progress.computed}</strong></span>
          <span>{t("discovery.cacheHits")}: <strong>{progress.cacheHits}</strong></span>
          <span>{t("discovery.skipped")}: <strong>{progress.skipped}</strong></span>
          {#if progress.errors > 0}
            <span class="err-count">{t("discovery.errors")}: <strong>{progress.errors}</strong></span>
          {/if}
        </div>
      </div>
      <div class="progress-track">
        <div class="progress-fill" style="width: {progressPct}%"></div>
      </div>
    </section>

    <section class="toolbar">
      <div class="toolbar-left">
        <input
          class="search-input"
          type="text"
          placeholder={t("discovery.filterSearch")}
          bind:value={searchText}
        />
        <label class="inline-field">
          <span>{t("discovery.filterMinScore")}</span>
          <select bind:value={minScore}>
            <option value={0}>0.0+</option>
            <option value={0.3}>0.3+</option>
            <option value={0.4}>0.4+</option>
            <option value={0.5}>0.5+</option>
            <option value={0.6}>0.6+</option>
            <option value={0.7}>0.7+</option>
            <option value={0.8}>0.8+</option>
          </select>
        </label>
        <select class="compact-select" bind:value={algFilter}>
          <option value="all">{t("discovery.filterAllAlgs")}</option>
          <option value="pearson">Pearson</option>
          <option value="spearman">Spearman</option>
          <option value="cross_correlation">Cross-Corr</option>
        </select>
        <label class="inline-field">
          <span>{t("discovery.sortBy")}</span>
          <select bind:value={sortBy}>
            <option value="score">{t("discovery.sortScore")}</option>
            <option value="pvalue">{t("discovery.sortPValue")}</option>
            <option value="samples">{t("discovery.sortSamples")}</option>
            <option value="updated">{t("discovery.sortUpdated")}</option>
          </select>
        </label>
        <label class="filter-check">
          <input type="checkbox" bind:checked={excludeSameLocation} />
          <span>{t("discovery.filterExcludeSameLoc")}</span>
        </label>
        <label class="filter-check">
          <input type="checkbox" bind:checked={excludeIdentical} />
          <span>{t("discovery.filterExcludeIdentical")}</span>
        </label>
        <label class="filter-check">
          <input type="checkbox" bind:checked={dedupePairs} />
          <span>{t("discovery.filterDedupePairs")}</span>
        </label>
      </div>

      <div class="toolbar-right">
        <span class="count-pill">
          {t("discovery.filteredCount")}: {filteredItems.length} / {t("discovery.totalCached")}: {items.length}
        </span>
        {#if checkedKeys.length > 0}
          <button class="chip-btn" onclick={() => rerunKeys(checkedKeys)}>
            {t("discovery.rerunSelected")} ({checkedKeys.length})
          </button>
          <button class="danger-btn" onclick={() => deleteKeys(checkedKeys)}>
            {t("discovery.deleteSelected")} ({checkedKeys.length})
          </button>
        {/if}
        {#if confirmClear}
          <button class="danger-btn" onclick={clearAllCache}>{t("settings.confirmDelete")}</button>
          <button class="chip-btn" onclick={() => (confirmClear = false)}>Cancel</button>
        {:else if items.length > 0}
          <button class="danger-outline-btn" onclick={() => (confirmClear = true)}>
            {t("discovery.clearAll")}
          </button>
        {/if}
      </div>
    </section>

    <div class="table-wrap">
      {#if filteredItems.length === 0}
        <div class="empty-state">
          <p>{t("discovery.empty")}</p>
        </div>
      {:else}
        <table class="disc-table">
          <thead>
            <tr>
              <th class="col-chk"></th>
              <th>{t("discovery.colScore")}</th>
              <th>{t("discovery.colPair")}</th>
              <th>{t("discovery.colAlg")}</th>
              <th>{t("discovery.colLag")}</th>
              <th>{t("discovery.colPValue")}</th>
              <th>{t("discovery.colSamples")}</th>
              <th class="col-act">{t("discovery.colActions")}</th>
            </tr>
          </thead>
          <tbody>
            {#each filteredItems as item (item.key)}
              <tr onclick={() => inspectItem(item.key)}>
                <td class="col-chk" onclick={(e) => e.stopPropagation()}>
                  <input
                    type="checkbox"
                    checked={!!selectedKeys[item.key]}
                    onchange={(e) => {
                      selectedKeys = { ...selectedKeys, [item.key]: e.currentTarget.checked };
                    }}
                  />
                </td>
                <td>
                  <span class="score-pill {scoreClass(item.score)}">
                    {item.score.toFixed(3)}
                  </span>
                </td>
                <td class="pair-cell">
                  <div class="pair-x">{formatSeriesLabel(item.request.seriesX.kind, item.request.seriesX.locationId, item.request.seriesX.metric)}</div>
                  <div class="pair-y">{formatSeriesLabel(item.request.seriesY.kind, item.request.seriesY.locationId, item.request.seriesY.metric)}</div>
                </td>
                <td>
                  <div class="alg-badge">{algShortLabel(item.request.algorithm)}</div>
                  <div class="prep-tags">
                    <span>{item.request.rangeKey}/{item.request.granularity}</span>
                    {#if item.request.preprocess?.deseasonalize}
                      <span class="tag-anom">Anomaly</span>
                    {/if}
                    {#if item.request.preprocess?.difference}
                      <span class="tag-diff">Diff</span>
                    {/if}
                  </div>
                </td>
                <td>
                  {#if item.request.algorithm === "cross_correlation"}
                    <span class="lag-val">
                      {item.metrics.bestLag > 0 ? `+${item.metrics.bestLag}` : item.metrics.bestLag}
                      ({item.metrics.bestLagCorrelation.toFixed(3)})
                    </span>
                  {:else}
                    <span class="muted">—</span>
                  {/if}
                </td>
                <td>
                  {#if item.request.algorithm !== "cross_correlation"}
                    {item.metrics.pValue.toExponential(1)}
                  {:else}
                    <span class="muted">—</span>
                  {/if}
                </td>
                <td>{item.sampleCount}</td>
                <td class="col-act" onclick={(e) => e.stopPropagation()}>
                  <div class="row-actions">
                    <button class="chip-btn" onclick={() => inspectItem(item.key)}>{t("discovery.inspect")}</button>
                    <button class="mini-btn" onclick={() => rerunKeys([item.key])} title={t("discovery.rerunOne")}>↻</button>
                    <button class="mini-btn danger" onclick={() => deleteKeys([item.key])} title={t("discovery.deleteOne")}>✕</button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </main>
</div>

<style>
  .disc-root {
    display: grid;
    grid-template-columns: 310px 1fr;
    gap: 12px;
    height: 100%;
    min-height: 0;
    color: #e2e8f0;
  }

  .disc-sidebar {
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

  .subtitle {
    margin: 4px 0 0;
    font-size: 11px;
    color: #94a3b8;
    line-height: 1.4;
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

  .sec-sub {
    font-size: 11px;
    color: #94a3b8;
    margin-top: 2px;
  }

  .check-group {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .row-2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }

  select,
  input[type="number"],
  .search-input {
    background: #0f172a;
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 5px 7px;
    font-size: 12px;
  }

  select,
  input[type="number"] {
    width: 100%;
    box-sizing: border-box;
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
  .run-btn:hover {
    background: #0ea5e9;
  }

  .pause-btn {
    background: #d97706;
    color: #f8fafc;
    border: none;
    border-radius: 8px;
    padding: 9px 12px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }

  .secondary-btn {
    background: rgba(56, 189, 248, 0.14);
    color: #7dd3fc;
    border: 1px solid rgba(56, 189, 248, 0.3);
    border-radius: 8px;
    padding: 7px 10px;
    font-size: 12px;
    cursor: pointer;
  }

  .disc-main {
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

  .status-bar {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    background: rgba(30, 41, 59, 0.6);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 8px;
  }

  .status-top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 12px;
  }

  .status-left {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .status-badge {
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 600;
  }
  .status-idle {
    background: rgba(148, 163, 184, 0.2);
    color: #cbd5e1;
  }
  .status-running {
    background: rgba(14, 165, 233, 0.22);
    color: #38bdf8;
  }
  .status-paused {
    background: rgba(245, 158, 11, 0.22);
    color: #fbbf24;
  }
  .status-completed {
    background: rgba(34, 197, 94, 0.22);
    color: #4ade80;
  }
  .status-error {
    background: rgba(239, 68, 68, 0.22);
    color: #f87171;
  }

  .current-pair {
    color: #94a3b8;
    font-size: 11px;
    max-width: 340px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status-metrics {
    display: flex;
    gap: 12px;
    font-size: 11px;
    color: #94a3b8;
  }
  .status-metrics strong {
    color: #e2e8f0;
  }
  .err-count {
    color: #fca5a5;
  }

  .progress-track {
    height: 6px;
    background: rgba(15, 23, 42, 0.8);
    border-radius: 999px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, #0284c7, #38bdf8);
    transition: width 0.2s ease;
  }

  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }

  .toolbar-left,
  .toolbar-right {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }

  .search-input {
    width: 190px;
  }

  .inline-field {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    color: #94a3b8;
  }

  .filter-check {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    color: #cbd5e1;
    cursor: pointer;
    padding: 3px 6px;
    background: rgba(30, 41, 59, 0.55);
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 6px;
  }

  .inline-field select,
  .compact-select {
    width: auto;
  }

  .count-pill {
    font-size: 11px;
    color: #94a3b8;
    padding: 0 4px;
  }

  .chip-btn {
    background: rgba(56, 189, 248, 0.14);
    color: #7dd3fc;
    border: 1px solid rgba(56, 189, 248, 0.3);
    border-radius: 6px;
    padding: 4px 8px;
    font-size: 11px;
    cursor: pointer;
  }
  .chip-btn:hover {
    background: rgba(56, 189, 248, 0.25);
  }

  .danger-btn {
    background: rgba(239, 68, 68, 0.22);
    color: #fecaca;
    border: 1px solid rgba(239, 68, 68, 0.45);
    border-radius: 6px;
    padding: 4px 8px;
    font-size: 11px;
    cursor: pointer;
  }

  .danger-outline-btn {
    background: transparent;
    color: #fca5a5;
    border: 1px solid rgba(239, 68, 68, 0.35);
    border-radius: 6px;
    padding: 4px 8px;
    font-size: 11px;
    cursor: pointer;
  }

  .table-wrap {
    flex: 1 1 auto;
    min-height: 0;
    overflow: auto;
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 8px;
  }

  .disc-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .disc-table th {
    position: sticky;
    top: 0;
    background: #0f172a;
    color: #94a3b8;
    font-weight: 600;
    font-size: 11px;
    text-align: left;
    padding: 8px 10px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.2);
    z-index: 1;
  }

  .disc-table td {
    padding: 8px 10px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.1);
    vertical-align: middle;
  }

  .disc-table tbody tr {
    cursor: pointer;
  }
  .disc-table tbody tr:hover {
    background: rgba(56, 189, 248, 0.08);
  }

  .col-chk {
    width: 28px;
  }
  .col-act {
    width: 140px;
  }

  .score-pill {
    display: inline-block;
    padding: 3px 8px;
    border-radius: 6px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .score-high {
    background: rgba(34, 197, 94, 0.2);
    color: #4ade80;
  }
  .score-mid {
    background: rgba(245, 158, 11, 0.2);
    color: #fbbf24;
  }
  .score-low {
    background: rgba(148, 163, 184, 0.16);
    color: #cbd5e1;
  }

  .pair-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .pair-x {
    color: #f8fafc;
    font-weight: 500;
  }
  .pair-y {
    color: #94a3b8;
    font-size: 11px;
  }

  .alg-badge {
    font-weight: 600;
    color: #e2e8f0;
  }

  .prep-tags {
    display: flex;
    gap: 4px;
    margin-top: 2px;
    font-size: 10px;
    color: #94a3b8;
  }
  .tag-anom {
    background: rgba(56, 189, 248, 0.15);
    color: #7dd3fc;
    padding: 0 4px;
    border-radius: 4px;
  }
  .tag-diff {
    background: rgba(168, 85, 247, 0.15);
    color: #d8b4fe;
    padding: 0 4px;
    border-radius: 4px;
  }

  .lag-val {
    color: #fbbf24;
    font-weight: 600;
  }
  .muted {
    color: #64748b;
  }

  .row-actions {
    display: flex;
    gap: 4px;
    align-items: center;
  }

  .mini-btn {
    background: rgba(148, 163, 184, 0.15);
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 5px;
    padding: 3px 6px;
    font-size: 11px;
    cursor: pointer;
  }
  .mini-btn.danger:hover {
    background: rgba(239, 68, 68, 0.25);
    color: #fecaca;
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 200px;
    color: #94a3b8;
    font-size: 13px;
    padding: 20px;
    text-align: center;
  }
</style>
