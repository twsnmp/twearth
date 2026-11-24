<script lang="ts">
  import { onMount } from "svelte";
  import {
    appState,
    arrangeAnalysisWindows,
    closeAllAnalysisWindows,
    getWindowId,
    getWindowView,
    hasUpdateBadge,
    initAppState,
    openAnalysisWindow,
    openLocationAI,
    refreshNow,
    setAnalysisWindowTitle,
    toggleLanguage,
    type MobileTab,
    type WindowLayout,
    type WindowView,
  } from "./lib/stores/appState.svelte";
  import { i18nState, t } from "./lib/i18n/index.svelte";
  import SettingsModal from "./lib/components/SettingsModal.svelte";
  import AIAssistantModal from "./lib/components/AIAssistantModal.svelte";
  import MapCanvas from "./lib/components/MapCanvas.svelte";
  import ChartsPanel from "./lib/components/ChartsPanel.svelte";
  import EventFeed from "./lib/components/EventFeed.svelte";
  import LocationStrip from "./lib/components/LocationStrip.svelte";
  import MarinePanel from "./lib/components/MarinePanel.svelte";
  import SynopticPanel from "./lib/components/SynopticPanel.svelte";
  import CorrelationPanel from "./lib/components/CorrelationPanel.svelte";

  const view: WindowView = getWindowView();
  const winId: string = getWindowId();
  let windowMenuOpen = $state(false);

  onMount(() => {
    void initAppState();
  });

  $effect(() => {
    if (view === "main" || !winId || appState.platform.isMobile) return;
    const base = t(`windows.${view}`);
    const title = appState.windowSubtitle ? `twearth — ${base} (${appState.windowSubtitle})` : `twearth — ${base}`;
    void setAnalysisWindowTitle(winId, title);
  });

  function setTab(tab: MobileTab) {
    appState.activeTab = tab;
  }

  function openSettings() {
    if (hasUpdateBadge() && appState.platform.canSelfUpdate) {
      appState.settingsTab = "update";
    }
    appState.settingsOpen = true;
  }

  async function handleOpenWindow(target: Exclude<WindowView, "main">) {
    windowMenuOpen = false;
    await openAnalysisWindow(target);
  }

  async function handleArrangeWindows(layout: WindowLayout) {
    windowMenuOpen = false;
    await arrangeAnalysisWindows(layout);
  }

  async function handleCloseAllWindows() {
    windowMenuOpen = false;
    await closeAllAnalysisWindows();
  }
</script>

<svelte:window onclick={() => (windowMenuOpen = false)} />

<div class="shell" class:is-mobile={appState.platform.isMobile}>
  <header class="topbar" class:mac-inset={!appState.platform.isMobile && appState.platform.os === "darwin"}>
    <div class="brand">
      <span class="dot"></span>
      <span class="title">{t("app.title")}</span>
      <span class="badge">{appState.platform.isMobile ? t("app.mobile") : t("app.desktop")}</span>
      {#if view !== "main"}
        <span class="view-badge">{t(`windows.${view}`)}</span>
        {#if appState.windowSubtitle}
          <span class="view-subtitle" title={appState.windowSubtitle}>{appState.windowSubtitle}</span>
        {/if}
      {/if}
    </div>

    <div class="top-actions">
      <button
        class="icon-btn ai-btn"
        title={t("ai.assistantTitle")}
        aria-label={t("ai.assistantTitle")}
        onclick={() => openLocationAI()}
      >
        <span aria-hidden="true">✨</span>
        <span class="ai-label">{t("ai.button")}</span>
      </button>

      <button
        class="icon-btn"
        title={t("app.refresh")}
        aria-label={t("app.refresh")}
        onclick={() => refreshNow()}
      >
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M21 12a9 9 0 1 1-2.64-6.36" />
          <polyline points="21 3 21 9 15 9" />
        </svg>
      </button>

      {#if !appState.platform.isMobile}
        <div
          class="win-menu-wrap"
          role="presentation"
          onclick={(e) => e.stopPropagation()}
        >
          <button
            class="icon-btn"
            title={t("nav.windows")}
            aria-label={t("nav.windows")}
            onclick={() => (windowMenuOpen = !windowMenuOpen)}
          >
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="8" height="8" rx="1.5" />
              <rect x="13" y="3" width="8" height="8" rx="1.5" />
              <rect x="3" y="13" width="8" height="8" rx="1.5" />
              <rect x="13" y="13" width="8" height="8" rx="1.5" />
            </svg>
          </button>
          {#if windowMenuOpen}
            <div class="win-dropdown">
              <div class="win-section-label">{t("windows.openNew")}</div>
              <button onclick={() => handleOpenWindow("charts")}>{t("windows.charts")}</button>
              <button onclick={() => handleOpenWindow("synoptic")}>{t("windows.synoptic")}</button>
              <button onclick={() => handleOpenWindow("marine")}>{t("windows.marine")}</button>
              <button onclick={() => handleOpenWindow("events")}>{t("windows.events")}</button>
              <button onclick={() => handleOpenWindow("correlation")}>{t("windows.correlation")}</button>
              <div class="win-divider"></div>
              <div class="win-section-label">{t("windows.arrangeSection")}</div>
              <button onclick={() => handleArrangeWindows("grid")}>{t("windows.arrangeGrid")}</button>
              <button onclick={() => handleArrangeWindows("horizontal")}>{t("windows.arrangeHorizontal")}</button>
              <button onclick={() => handleArrangeWindows("vertical")}>{t("windows.arrangeVertical")}</button>
              <button onclick={() => handleArrangeWindows("cascade")}>{t("windows.arrangeCascade")}</button>
              <button class="danger" onclick={() => handleCloseAllWindows()}>{t("windows.closeAll")}</button>
            </div>
          {/if}
        </div>
      {/if}

      <button
        class="icon-btn lang-btn"
        title={t("nav.language")}
        aria-label={t("nav.language")}
        onclick={() => toggleLanguage()}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="9" />
          <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
        </svg>
        <span class="lang-code">{i18nState.lang.toUpperCase()}</span>
      </button>

      <button
        class="icon-btn"
        title={t("nav.settings")}
        aria-label={t("nav.settings")}
        onclick={openSettings}
      >
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="3" />
          <path
            d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.85 1 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
          />
        </svg>
        {#if hasUpdateBadge()}
          <span class="update-badge" aria-label={t("settings.updateAvailable")}></span>
        {/if}
      </button>
    </div>
  </header>

  <main class="workspace">
    {#if appState.error}
      <div class="error-banner">{appState.error}</div>
    {/if}

    {#if view === "charts"}
      <LocationStrip />
      <section class="panel full-panel">
        <ChartsPanel />
      </section>
    {:else if view === "synoptic"}
      <section class="panel full-panel">
        <SynopticPanel />
      </section>
    {:else if view === "marine"}
      <section class="panel full-panel">
        <MarinePanel />
      </section>
    {:else if view === "events"}
      <section class="panel full-panel">
        <EventFeed />
      </section>
    {:else if view === "correlation" && !appState.platform.isMobile}
      <section class="panel full-panel">
        <CorrelationPanel />
      </section>
    {:else}
      <LocationStrip />

      {#if appState.platform.isMobile}
        {#if appState.activeTab === "map"}
          <section class="panel map-panel">
            <MapCanvas />
          </section>
        {:else if appState.activeTab === "charts"}
          <section class="panel">
            <ChartsPanel />
          </section>
        {:else if appState.activeTab === "marine"}
          <section class="panel">
            <MarinePanel />
          </section>
        {:else}
          <section class="panel">
            <EventFeed />
          </section>
        {/if}
      {:else}
        <div class="desktop-grid">
          <section class="panel map-panel">
            <MapCanvas />
          </section>
          <section class="panel charts-bottom-panel">
            <ChartsPanel horizontal={true} />
          </section>
          <aside class="panel events-side-panel">
            <EventFeed />
          </aside>
        </div>
      {/if}
    {/if}
  </main>

  {#if appState.platform.isMobile && view === "main"}
    <nav class="mobile-nav" aria-label="Mobile Navigation">
      <button class:active={appState.activeTab === "map"} onclick={() => setTab("map")}>{t("nav.map")}</button>
      <button class:active={appState.activeTab === "charts"} onclick={() => setTab("charts")}>{t("nav.charts")}</button>
      <button class:active={appState.activeTab === "marine"} onclick={() => setTab("marine")}>{t("nav.marine")}</button>
      <button class:active={appState.activeTab === "events"} onclick={() => setTab("events")}>{t("nav.events")}</button>
    </nav>
  {/if}

  {#if appState.settingsOpen}
    <div class="modal-backdrop" role="dialog" aria-modal="true">
      <div class="modal-wrap" class:mobile-wrap={appState.platform.isMobile}>
        <SettingsModal inline={appState.platform.isMobile} onClose={() => (appState.settingsOpen = false)} />
      </div>
    </div>
  {/if}

  {#if appState.aiModal.open}
    <div class="modal-backdrop" role="dialog" aria-modal="true">
      <div class="modal-wrap ai-modal-wrap" class:mobile-wrap={appState.platform.isMobile}>
        <AIAssistantModal />
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
    padding: 8px 14px;
    background: #0b1020;
    border-bottom: 1px solid rgba(56, 189, 248, 0.2);
    -webkit-app-region: drag;
  }
  .topbar.mac-inset {
    padding-left: 78px;
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
    letter-spacing: 0.04em;
  }
  .badge,
  .view-badge {
    font-size: 0.65rem;
    padding: 2px 6px;
    border-radius: 4px;
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
  }
  .view-badge {
    background: rgba(45, 212, 191, 0.15);
    color: #2dd4bf;
  }
  .view-subtitle {
    font-size: 0.72rem;
    color: #94a3b8;
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .top-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    -webkit-app-region: no-drag;
  }
  .icon-btn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    background: #131c31;
    color: #cbd5e1;
    border: 1px solid rgba(148, 163, 184, 0.24);
    border-radius: 8px;
    padding: 6px 8px;
    cursor: pointer;
    font-size: 0.72rem;
  }
  .icon-btn:hover {
    border-color: rgba(56, 189, 248, 0.6);
    color: #38bdf8;
  }
  .lang-code {
    font-weight: 700;
    font-size: 0.68rem;
  }
  .update-badge {
    position: absolute;
    top: -3px;
    right: -3px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #f43f5e;
    border: 1.5px solid #0b1020;
    box-shadow: 0 0 6px #f43f5e;
  }
  .win-menu-wrap {
    position: relative;
  }
  .win-dropdown {
    position: absolute;
    right: 0;
    top: calc(100% + 6px);
    background: #0d1222;
    border: 1px solid rgba(56, 189, 248, 0.3);
    border-radius: 8px;
    padding: 6px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 210px;
    z-index: 40;
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.45);
  }
  .win-dropdown button {
    background: transparent;
    border: none;
    color: #e2e8f0;
    padding: 7px 10px;
    text-align: left;
    border-radius: 6px;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .win-dropdown button:hover {
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
  }
  .win-dropdown button.danger {
    color: #fda4af;
  }
  .win-dropdown button.danger:hover {
    background: rgba(244, 63, 94, 0.16);
    color: #fb7185;
  }
  .win-section-label {
    font-size: 0.66rem;
    color: #64748b;
    padding: 4px 10px 2px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .win-divider {
    height: 1px;
    background: rgba(148, 163, 184, 0.18);
    margin: 3px 4px;
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
    grid-template-columns: minmax(0, 1fr) 400px;
    grid-template-rows: minmax(0, 1fr) clamp(220px, 31vh, 290px);
    gap: 10px;
    padding: 10px;
    min-height: 0;
  }
  .map-panel {
    grid-column: 1;
    grid-row: 1;
  }
  .charts-bottom-panel {
    grid-column: 1;
    grid-row: 2;
  }
  .events-side-panel {
    grid-column: 2;
    grid-row: 1 / 3;
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
  .full-panel {
    margin: 10px;
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
    font-size: 0.8rem;
    cursor: pointer;
  }
  .mobile-nav button.active {
    color: #38bdf8;
    font-weight: 600;
  }
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(2, 6, 23, 0.78);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
    z-index: 50;
  }
  .modal-wrap {
    width: min(720px, 100%);
  }
  .modal-wrap.ai-modal-wrap {
    width: min(820px, 100%);
  }
  .ai-btn {
    border-color: rgba(56, 189, 248, 0.4);
    color: #bae6fd;
  }
  .ai-label {
    font-weight: 600;
    font-size: 0.72rem;
  }
  .modal-wrap.mobile-wrap {
    width: 100%;
    height: 92vh;
  }
  .error-banner {
    background: rgba(239, 68, 68, 0.2);
    color: #fca5a5;
    padding: 8px 14px;
    font-size: 0.85rem;
  }
</style>
