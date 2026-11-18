<script lang="ts">
  import { onMount } from "svelte";
  import { appState, initAppState, type MobileTab } from "./lib/stores/appState.svelte";
  import SettingsModal from "./lib/components/SettingsModal.svelte";
  import MapCanvas from "./lib/components/MapCanvas.svelte";
  import ChartsPanel from "./lib/components/ChartsPanel.svelte";
  import EventFeed from "./lib/components/EventFeed.svelte";

  onMount(() => {
    void initAppState();
  });

  function setTab(tab: MobileTab) {
    appState.activeTab = tab;
  }
</script>

<div class="shell" class:is-mobile={appState.platform.isMobile}>
  <header class="topbar" class:mac-inset={!appState.platform.isMobile && appState.platform.os === "darwin"}>
    <div class="brand">
      <span class="dot"></span>
      <span class="title">TWSNMP Earth</span>
      <span class="badge">{appState.platform.isMobile ? "MOBILE" : "DESKTOP"}</span>
    </div>

    <div class="top-meta">
      {#if appState.config}
        <span class="loc">{appState.config.locationName} ({appState.config.latitude.toFixed(2)}, {appState.config.longitude.toFixed(2)})</span>
      {/if}
      {#if !appState.platform.isMobile}
        <button class="settings-btn" onclick={() => (appState.settingsOpen = true)}>設定</button>
      {/if}
    </div>
  </header>

  <main class="workspace">
    {#if appState.error}
      <div class="error-banner">{appState.error}</div>
    {/if}

    {#if appState.platform.isMobile}
      {#if appState.activeTab === "map"}
        <section class="panel map-panel">
          <MapCanvas />
        </section>
      {:else if appState.activeTab === "charts"}
        <section class="panel">
          <ChartsPanel />
        </section>
      {:else if appState.activeTab === "events"}
        <section class="panel">
          <EventFeed />
        </section>
      {:else}
        <section class="panel">
          <SettingsModal inline={true} />
        </section>
      {/if}
    {:else}
      <div class="desktop-grid">
        <section class="panel map-panel">
          <MapCanvas />
        </section>
        <aside class="side-panels">
          <section class="panel">
            <ChartsPanel />
          </section>
          <section class="panel">
            <EventFeed />
          </section>
        </aside>
      </div>
    {/if}
  </main>

  {#if appState.platform.isMobile}
    <nav class="mobile-nav" aria-label="モバイルナビゲーション">
      <button class:active={appState.activeTab === "map"} onclick={() => setTab("map")}>地図</button>
      <button class:active={appState.activeTab === "charts"} onclick={() => setTab("charts")}>グラフ</button>
      <button class:active={appState.activeTab === "events"} onclick={() => setTab("events")}>イベント</button>
      <button class:active={appState.activeTab === "settings"} onclick={() => setTab("settings")}>設定</button>
    </nav>
  {/if}

  {#if appState.settingsOpen && !appState.platform.isMobile}
    <div class="modal-backdrop" role="dialog" aria-modal="true">
      <div class="modal-wrap">
        <SettingsModal onClose={() => (appState.settingsOpen = false)} />
      </div>
    </div>
  {/if}
</div>

<style>
  :global(html, body, #app) {
    margin: 0;
    padding: 0;
    height: 100%;
    background: #06070f;
    color: #e2e8f0;
    font-family: system-ui, -apple-system, sans-serif;
  }
  .shell {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding-top: env(safe-area-inset-top);
  }
  .topbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 16px;
    background: #0b1020;
    border-bottom: 1px solid rgba(56, 189, 248, 0.2);
  }
  .topbar.mac-inset {
    padding-left: 80px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #38bdf8;
    box-shadow: 0 0 8px #38bdf8;
  }
  .title {
    font-weight: 700;
    letter-spacing: 0.03em;
  }
  .badge {
    font-size: 0.65rem;
    padding: 2px 6px;
    border-radius: 4px;
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
  }
  .top-meta {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 0.82rem;
    color: #94a3b8;
  }
  .settings-btn {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 5px 12px;
    cursor: pointer;
  }
  .workspace {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .desktop-grid {
    flex: 1;
    display: grid;
    grid-template-columns: 1fr 380px;
    gap: 12px;
    padding: 12px;
    min-height: 0;
  }
  .side-panels {
    display: grid;
    grid-template-rows: 1fr 1fr;
    gap: 12px;
    min-height: 0;
  }
  .panel {
    background: #0d1222;
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 10px;
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
  }
  .placeholder {
    margin: auto;
    color: #64748b;
    font-size: 0.9rem;
    padding: 20px;
    text-align: center;
  }
  .mobile-nav {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    background: #0b1020;
    border-top: 1px solid rgba(56, 189, 248, 0.2);
    padding-bottom: env(safe-area-inset-bottom);
  }
  .mobile-nav button {
    background: transparent;
    border: none;
    color: #94a3b8;
    padding: 12px 4px;
    font-size: 0.82rem;
    cursor: pointer;
  }
  .mobile-nav button.active {
    color: #38bdf8;
    font-weight: 600;
  }
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(2, 6, 23, 0.75);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
    z-index: 50;
  }
  .modal-wrap {
    width: min(640px, 100%);
  }
  .error-banner {
    background: rgba(239, 68, 68, 0.2);
    color: #fca5a5;
    padding: 8px 14px;
    font-size: 0.85rem;
  }
</style>
