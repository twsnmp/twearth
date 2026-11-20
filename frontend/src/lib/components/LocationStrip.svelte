<script lang="ts">
  import { appState, selectLocation } from "../stores/appState.svelte";
  import { locDisplayName } from "../i18n/index.svelte";

  const locations = $derived(appState.config?.locations ?? []);

  function trendArrow(delta: number): string {
    if (delta >= 1.5) return "↗";
    if (delta <= -1.5) return "↘";
    return "→";
  }
</script>

{#if locations.length > 0}
  <div class="strip" class:mobile={appState.platform.isMobile}>
    {#each locations as loc (loc.id)}
      {@const w = appState.weathers.find((item) => item.locationId === loc.id)}
      {@const active = loc.id === appState.selectedLocationId}
      <button class="loc-card" class:active onclick={() => selectLocation(loc.id, true)}>
        <div class="loc-head">
          <span class="loc-name">{locDisplayName(loc)}</span>
          {#if loc.source === "current"}
            <span class="tag">GPS</span>
          {/if}
        </div>
        {#if w}
          <div class="loc-metrics">
            <span class="temp">{w.temperature2m.toFixed(1)}°C</span>
            <span class="press" class:drop={w.pressureDelta3h <= -2.0}>
              {w.pressureMsl.toFixed(0)}hPa {trendArrow(w.pressureDelta3h)}
            </span>
            {#if w.snowfall > 0}
              <span class="snow">❄{w.snowfall.toFixed(1)}cm</span>
            {:else if w.precipitation > 0}
              <span class="rain">💧{w.precipitation.toFixed(1)}mm</span>
            {/if}
          </div>
        {:else}
          <div class="loc-metrics muted">--</div>
        {/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .strip {
    display: flex;
    gap: 8px;
    padding: 6px 12px;
    background: #080d1a;
    border-bottom: 1px solid rgba(56, 189, 248, 0.14);
    overflow-x: auto;
    scrollbar-width: thin;
  }
  .strip.mobile {
    padding: 6px 10px;
    gap: 6px;
  }
  .loc-card {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 118px;
    padding: 5px 10px;
    border-radius: 8px;
    background: rgba(15, 23, 42, 0.75);
    border: 1px solid rgba(148, 163, 184, 0.2);
    color: #cbd5e1;
    cursor: pointer;
    text-align: left;
    transition: border-color 0.15s ease, background 0.15s ease;
  }
  .loc-card.active {
    background: rgba(14, 116, 144, 0.25);
    border-color: rgba(56, 189, 248, 0.7);
    color: #f8fafc;
  }
  .loc-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    font-size: 0.74rem;
    font-weight: 600;
  }
  .tag {
    font-size: 0.6rem;
    padding: 1px 4px;
    border-radius: 4px;
    background: rgba(56, 189, 248, 0.2);
    color: #38bdf8;
  }
  .loc-metrics {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-size: 0.74rem;
  }
  .temp {
    color: #fbbf24;
    font-weight: 600;
  }
  .rain {
    color: #60a5fa;
    font-weight: 600;
  }
  .snow {
    color: #e0f2fe;
    font-weight: 600;
  }
  .press {
    color: #94a3b8;
    font-size: 0.7rem;
  }
  .press.drop {
    color: #fb7185;
  }
  .muted {
    color: #64748b;
  }
</style>
