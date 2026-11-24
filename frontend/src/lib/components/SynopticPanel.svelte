<script lang="ts">
  import MapCanvas from "./MapCanvas.svelte";
  import { appState, focusMapAt, openViewAI } from "../stores/appState.svelte";
  import { t } from "../i18n/index.svelte";

  const isobarCount = $derived(appState.synoptic?.isobars?.length ?? 0);
  const centers = $derived(appState.synoptic?.centers ?? []);
</script>

<div class="synoptic-view">
  <div class="syn-map">
    <MapCanvas />
  </div>
  <aside class="syn-side">
    <div class="syn-head">
      <h3>{t("charts.synopticSummary")}</h3>
      <button class="ai-btn" type="button" onclick={() => openViewAI({ view: "synoptic" })}>
        ✨ {t("ai.explainViewBtn")}
      </button>
    </div>
    <div class="stat-row">
      <span>{t("charts.isobarCount")}</span>
      <strong>{isobarCount}</strong>
    </div>
    <div class="stat-row">
      <span>{t("charts.centersCount")}</span>
      <strong>{centers.length}</strong>
    </div>
    <div class="center-list">
      {#each centers as c, idx (idx)}
        <button
          class="center-item"
          class:high={c.kind === "H"}
          class:low={c.kind === "L"}
          onclick={() => focusMapAt(c.latitude, c.longitude, 5.5)}
        >
          <span class="badge">{c.kind}</span>
          <span class="press">{c.pressure.toFixed(1)} hPa</span>
          <span class="coord">{c.latitude.toFixed(1)}, {c.longitude.toFixed(1)}</span>
        </button>
      {/each}
    </div>
  </aside>
</div>

<style>
  .synoptic-view {
    display: grid;
    grid-template-columns: 1fr 270px;
    height: 100%;
    gap: 10px;
    padding: 10px;
    box-sizing: border-box;
  }
  .syn-map {
    border-radius: 10px;
    overflow: hidden;
    border: 1px solid rgba(56, 189, 248, 0.22);
    min-height: 0;
  }
  .syn-side {
    background: #0d1222;
    border: 1px solid rgba(148, 163, 184, 0.18);
    border-radius: 10px;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow-y: auto;
  }
  h3 {
    margin: 0;
    font-size: 0.9rem;
    color: #e2e8f0;
  }
  .syn-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }
  .ai-btn {
    background: rgba(14, 116, 144, 0.3);
    border: 1px solid rgba(56, 189, 248, 0.45);
    color: #bae6fd;
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 0.72rem;
    cursor: pointer;
  }
  .ai-btn:hover {
    border-color: #38bdf8;
    color: #ffffff;
  }
  .stat-row {
    display: flex;
    justify-content: space-between;
    font-size: 0.8rem;
    color: #94a3b8;
    padding: 6px 8px;
    background: rgba(15, 23, 42, 0.7);
    border-radius: 6px;
  }
  .stat-row strong {
    color: #38bdf8;
  }
  .center-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .center-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 7px 10px;
    border-radius: 7px;
    border: 1px solid rgba(148, 163, 184, 0.2);
    background: rgba(15, 23, 42, 0.65);
    color: #e2e8f0;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .center-item.high .badge {
    color: #38bdf8;
    font-weight: 700;
  }
  .center-item.low .badge {
    color: #f43f5e;
    font-weight: 700;
  }
  .coord {
    color: #64748b;
    font-size: 0.72rem;
  }
</style>
