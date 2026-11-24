<script lang="ts">
  import {
    appState,
    checkForUpdate,
    detectCurrentLocation,
    installUpdate,
    restartApp,
    saveAppConfig,
    searchCities,
  } from "../stores/appState.svelte";
  import type {
    Config,
    MarineLocation,
    ObservationLocation,
  } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import type { CitySearchResult } from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";
  import {
    CITY_PRESETS,
    MARINE_PRESETS,
    cityPresetToLocation,
    marinePresetToLocation,
  } from "../presets";
  import { locDisplayName, t } from "../i18n/index.svelte";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type { AIHardwareStatus } from "../../../bindings/github.com/twsnmp/twearth/internal/app/models";
  import type { ModelInfo } from "../../../bindings/github.com/twsnmp/twearth/internal/ai/model/models";
  import ModelManagerModal from "./ModelManagerModal.svelte";

  interface Props {
    inline?: boolean;
    onClose?: () => void;
  }

  let { inline = false, onClose }: Props = $props();

  let draft = $state<Config | null>(null);
  let saving = $state(false);
  let cityQuery = $state("");
  let searching = $state(false);
  let searchResults = $state<CitySearchResult[]>([]);

  let customCityName = $state("");
  let customCityLat = $state(35.6762);
  let customCityLon = $state(139.6503);

  let customMarineName = $state("");
  let customMarineLat = $state(35.15);
  let customMarineLon = $state(139.35);

  let clearScope = $state<"all" | "weather" | "marine" | "quake">("all");
  let clearConfirmOpen = $state(false);
  let clearingHistory = $state(false);
  let clearFeedback = $state("");
  let syncingNow = $state(false);
  let autoMarineUpdating = $state(false);
  let showModelManager = $state(false);
  let localModels = $state<ModelInfo[]>([]);
  let hardwareStatus = $state<AIHardwareStatus | null>(null);

  async function loadLocalAIInfo() {
    try {
      const [models, hw] = await Promise.all([
        AppService.GetLocalModels(),
        AppService.GetAIHardwareStatus(),
      ]);
      localModels = models ?? [];
      hardwareStatus = hw;
      if (draft && draft.llmProvider === "tensai" && localModels.length > 0) {
        if (!draft.llmModel || !localModels.some((m) => m.name === draft?.llmModel)) {
          draft.llmModel = localModels[0].name;
        }
      }
    } catch {
      // Ignore transient errors
    }
  }

  async function applyAutoMarinePreview() {
    if (!draft) return;
    autoMarineUpdating = true;
    try {
      const next = await AppService.PreviewAutoMarineLocations($state.snapshot(draft));
      if (Array.isArray(next)) {
        draft.marineLocations = next;
      }
    } finally {
      autoMarineUpdating = false;
    }
  }

  function marineCategoryLabel(cat?: string): string {
    switch (cat) {
      case "nearshore":
        return t("location.marineCatNearshore");
      case "climate":
        return t("location.marineCatClimate");
      default:
        return t("location.marineCatRegional");
    }
  }

  async function refreshHistorySyncStatus() {
    try {
      const list = await AppService.GetHistorySyncStatus();
      appState.historySyncStatuses = list ?? [];
    } catch {
      // Ignore transient errors
    }
  }

  async function handleSyncNow() {
    syncingNow = true;
    try {
      if (draft) {
        await saveAppConfig($state.snapshot(draft));
      } else {
        await AppService.TriggerHistorySync();
      }
      await new Promise((r) => setTimeout(r, 300));
      await refreshHistorySyncStatus();
    } finally {
      syncingNow = false;
    }
  }

  async function handleClearHistory(resync: boolean) {
    clearingHistory = true;
    clearFeedback = "";
    try {
      await AppService.ClearHistoryData(clearScope, resync);
      clearConfirmOpen = false;
      clearFeedback = t("settings.historyCleared");
      await refreshHistorySyncStatus();
    } finally {
      clearingHistory = false;
    }
  }

  $effect(() => {
    if (appState.settingsOpen && appState.settingsTab === "ai") {
      void loadLocalAIInfo();
    }
  });

  $effect(() => {
    if (appState.config && !draft) {
      const cloned = structuredClone($state.snapshot(appState.config));
      if (!cloned.alertDistanceMode) cloned.alertDistanceMode = "nearest";
      if (!cloned.alertNearRadiusKm || cloned.alertNearRadiusKm <= 0) cloned.alertNearRadiusKm = 100;
      if (!cloned.alertMediumRadiusKm || cloned.alertMediumRadiusKm <= 0) cloned.alertMediumRadiusKm = 300;
      draft = cloned;
    }
  });

  $effect(() => {
    if (appState.pickedCoord) {
      customCityLat = appState.pickedCoord.latitude;
      customCityLon = appState.pickedCoord.longitude;
      customMarineLat = appState.pickedCoord.latitude;
      customMarineLon = appState.pickedCoord.longitude;
    }
  });

  function setPrimaryCity(id: string) {
    if (!draft) return;
    draft.locations = (draft.locations ?? []).map((l) => ({
      ...l,
      isPrimary: l.id === id,
    }));
    if (draft.autoMarineByPrimary) {
      void applyAutoMarinePreview();
    }
  }

  function removeCity(id: string) {
    if (!draft) return;
    const next = (draft.locations ?? []).filter((l) => l.id !== id);
    if (next.length === 0) return;
    if (!next.some((l) => l.isPrimary)) next[0].isPrimary = true;
    draft.locations = next;
  }

  function addPresetCity(presetId: string) {
    if (!draft) return;
    const list = draft.locations ?? [];
    if (list.some((l) => l.id === presetId)) return;
    const p = CITY_PRESETS.find((c) => c.id === presetId);
    if (!p) return;
    draft.locations = [...list, cityPresetToLocation(p, list.length, list.length === 0)];
  }

  function addCurrentLocationCandidate() {
    if (!draft || !appState.geoCandidate) return;
    const list = draft.locations ?? [];
    const existingIdx = list.findIndex((l) => l.source === "current");
    const cityLabel = appState.geoCandidate.city?.trim();
    const entry: ObservationLocation = {
      id: "current-location",
      name: cityLabel ? `現在地 (${cityLabel})` : "現在地",
      nameEn: cityLabel ? `Current (${cityLabel})` : "Current Location",
      latitude: appState.geoCandidate.latitude,
      longitude: appState.geoCandidate.longitude,
      source: "current",
      isPrimary: existingIdx >= 0 ? list[existingIdx].isPrimary : list.length === 0,
      order: existingIdx >= 0 ? existingIdx : list.length,
    };
    if (existingIdx >= 0) {
      const next = [...list];
      next[existingIdx] = entry;
      draft.locations = next;
    } else {
      draft.locations = [...list, entry];
    }
  }

  async function handleCitySearch() {
    if (!cityQuery.trim()) return;
    searching = true;
    try {
      searchResults = await searchCities(cityQuery);
    } finally {
      searching = false;
    }
  }

  function addSearchResult(res: CitySearchResult) {
    if (!draft) return;
    const id = `city-${res.id}`;
    const list = draft.locations ?? [];
    if (list.some((l) => l.id === id)) return;
    const label = res.admin1 ? `${res.name} (${res.admin1})` : res.name;
    draft.locations = [
      ...list,
      {
        id,
        name: label,
        nameEn: label,
        latitude: Number(res.latitude.toFixed(4)),
        longitude: Number(res.longitude.toFixed(4)),
        source: "search",
        isPrimary: list.length === 0,
        order: list.length,
      },
    ];
  }

  function addCustomCity() {
    if (!draft) return;
    const name = customCityName.trim() || `${customCityLat.toFixed(2)}, ${customCityLon.toFixed(2)}`;
    const list = draft.locations ?? [];
    draft.locations = [
      ...list,
      {
        id: `custom-${Date.now()}`,
        name,
        nameEn: name,
        latitude: Number(customCityLat),
        longitude: Number(customCityLon),
        source: "custom",
        isPrimary: list.length === 0,
        order: list.length,
      },
    ];
    customCityName = "";
  }

  function setPrimaryMarine(id: string) {
    if (!draft) return;
    draft.marineLocations = (draft.marineLocations ?? []).map((m) => ({
      ...m,
      isPrimary: m.id === id,
    }));
  }

  function removeMarine(id: string) {
    if (!draft) return;
    const next = (draft.marineLocations ?? []).filter((m) => m.id !== id);
    if (next.length === 0) return;
    if (!next.some((m) => m.isPrimary)) next[0].isPrimary = true;
    draft.marineLocations = next;
  }

  function addPresetMarine(presetId: string) {
    if (!draft) return;
    const list = draft.marineLocations ?? [];
    if (list.some((m) => m.id === presetId)) return;
    const p = MARINE_PRESETS.find((m) => m.id === presetId);
    if (!p) return;
    draft.marineLocations = [...list, marinePresetToLocation(p, list.length, list.length === 0)];
  }

  function addCustomMarine() {
    if (!draft) return;
    const name = customMarineName.trim() || `Sea ${customMarineLat.toFixed(2)}, ${customMarineLon.toFixed(2)}`;
    const list = draft.marineLocations ?? [];
    draft.marineLocations = [
      ...list,
      {
        id: `marine-${Date.now()}`,
        name,
        nameEn: name,
        latitude: Number(customMarineLat),
        longitude: Number(customMarineLon),
        region: "Custom",
        source: "custom",
        category: "regional",
        isPrimary: list.length === 0,
        order: list.length,
      } satisfies MarineLocation,
    ];
    customMarineName = "";
  }

  async function handleSave() {
    if (!draft) return;
    if (
      draft.alertNearRadiusKm &&
      draft.alertMediumRadiusKm &&
      draft.alertMediumRadiusKm < draft.alertNearRadiusKm
    ) {
      draft.alertMediumRadiusKm = draft.alertNearRadiusKm;
    }
    saving = true;
    try {
      await saveAppConfig(draft);
      onClose?.();
    } finally {
      saving = false;
    }
  }
</script>

<div class="settings-card" class:inline>
  <div class="settings-header">
    <h2>{t("settings.title")}</h2>
    {#if !inline && onClose}
      <button class="close-btn" onclick={onClose} aria-label={t("app.close")}>✕</button>
    {/if}
  </div>

  <div class="tab-bar">
    <button class:active={appState.settingsTab === "locations"} onclick={() => (appState.settingsTab = "locations")}>
      {t("settings.tabLocations")}
    </button>
    <button class:active={appState.settingsTab === "marine"} onclick={() => (appState.settingsTab = "marine")}>
      {t("settings.tabMarine")}
    </button>
    <button class:active={appState.settingsTab === "general"} onclick={() => (appState.settingsTab = "general")}>
      {t("settings.tabGeneral")}
    </button>
    <button
      class:active={appState.settingsTab === "ai"}
      onclick={() => {
        appState.settingsTab = "ai";
        void loadLocalAIInfo();
      }}
    >
      {t("settings.tabAI")}
    </button>
    {#if appState.platform.canSelfUpdate}
      <button class:active={appState.settingsTab === "update"} onclick={() => (appState.settingsTab = "update")}>
        {t("settings.tabUpdate")}
        {#if appState.updater.state === "available" || appState.updater.state === "ready"}
          <span class="mini-dot"></span>
        {/if}
      </button>
    {/if}
  </div>

  {#if draft}
    <div class="settings-body">
      {#if appState.settingsTab === "locations"}
        <section class="group">
          <div class="group-head">
            <h3>{t("location.currentLocationCandidate")}</h3>
            <button class="btn-sm" onclick={() => detectCurrentLocation()} disabled={appState.geoStatus === "fetching"}>
              {appState.geoStatus === "fetching" ? t("location.detecting") : t("location.detectCurrent")}
            </button>
          </div>
          {#if appState.geoCandidate}
            <div class="candidate-box">
              <span>
                {#if appState.geoCandidate.city}<strong>{appState.geoCandidate.city}</strong> · {/if}
                {appState.geoCandidate.latitude.toFixed(4)}, {appState.geoCandidate.longitude.toFixed(4)}
                {#if appState.geoCandidate.accuracy}
                  (±{appState.geoCandidate.accuracy}m · GPS)
                {:else}
                  (IP)
                {/if}
              </span>
              <button class="btn-primary-sm" onclick={addCurrentLocationCandidate}>
                {t("location.addCurrent")}
              </button>
            </div>
          {:else if appState.geoStatus === "denied"}
            <p class="hint">{t("location.currentDenied")}</p>
          {:else if appState.geoStatus === "unavailable"}
            <p class="hint">{t("location.currentUnavailable")}</p>
          {/if}
        </section>

        <section class="group">
          <h3>{t("location.cities")} ({(draft.locations ?? []).length})</h3>
          <div class="item-list">
            {#each draft.locations ?? [] as loc (loc.id)}
              <div class="loc-row">
                <div>
                  <strong>{locDisplayName(loc)}</strong>
                  <span class="coord">{loc.latitude.toFixed(3)}, {loc.longitude.toFixed(3)}</span>
                </div>
                <div class="row-actions">
                  {#if loc.isPrimary}
                    <span class="pill-primary">{t("app.primary")}</span>
                  {:else}
                    <button class="btn-sm" onclick={() => setPrimaryCity(loc.id)}>{t("app.setPrimary")}</button>
                  {/if}
                  {#if (draft.locations ?? []).length > 1}
                    <button class="btn-danger-sm" onclick={() => removeCity(loc.id)}>{t("app.remove")}</button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        </section>

        <section class="group">
          <h3>{t("location.presetsTitle")}</h3>
          <div class="preset-chips">
            {#each CITY_PRESETS as p (p.id)}
              {@const exists = (draft.locations ?? []).some((l) => l.id === p.id)}
              <button class="chip" class:added={exists} disabled={exists} onclick={() => addPresetCity(p.id)}>
                {locDisplayName(p)} {exists ? "✓" : "+"}
              </button>
            {/each}
          </div>
        </section>

        <section class="group">
          <div class="search-row">
            <input
              type="text"
              bind:value={cityQuery}
              placeholder={t("location.searchPlaceholder")}
              onkeydown={(e) => e.key === "Enter" && handleCitySearch()}
            />
            <button class="btn-sm" onclick={handleCitySearch} disabled={searching}>
              {searching ? t("location.searching") : t("location.searchBtn")}
            </button>
          </div>
          {#if searchResults.length > 0}
            <div class="search-results">
              {#each searchResults as r (r.id)}
                <button class="result-item" onclick={() => addSearchResult(r)}>
                  <span>{r.name} {r.admin1 ? `(${r.admin1})` : ""} — {r.country}</span>
                  <span class="coord">{r.latitude.toFixed(2)}, {r.longitude.toFixed(2)} +</span>
                </button>
              {/each}
            </div>
          {/if}
        </section>

        <section class="group">
          <h3>{t("location.customAdd")}</h3>
          <p class="hint">{t("location.clickMapHint")}</p>
          <div class="custom-grid">
            <input type="text" bind:value={customCityName} placeholder={t("location.nameLabel")} />
            <input type="number" step="0.0001" bind:value={customCityLat} placeholder={t("location.latLabel")} />
            <input type="number" step="0.0001" bind:value={customCityLon} placeholder={t("location.lonLabel")} />
            <button class="btn-sm" onclick={addCustomCity}>{t("app.add")}</button>
          </div>
        </section>
      {:else if appState.settingsTab === "marine"}
        <section class="group">
          <div class="group-head">
            <h3>{t("location.marineAutoTitle")}</h3>
            <button
              class="btn-sm"
              type="button"
              disabled={autoMarineUpdating}
              onclick={() => applyAutoMarinePreview()}
            >
              {autoMarineUpdating ? "..." : t("location.marineAutoApply")}
            </button>
          </div>
          <label class="check-row">
            <input
              type="checkbox"
              bind:checked={draft.autoMarineByPrimary}
              onchange={() => {
                if (draft?.autoMarineByPrimary) {
                  void applyAutoMarinePreview();
                }
              }}
            />
            <span>{t("location.marineAutoToggle")}</span>
          </label>
          <label class="check-row">
            <input
              type="checkbox"
              bind:checked={draft.autoMarineClimate}
              onchange={() => {
                if (draft?.autoMarineByPrimary) {
                  void applyAutoMarinePreview();
                }
              }}
            />
            <span>{t("location.marineClimateToggle")}</span>
          </label>
        </section>

        <section class="group">
          <h3>{t("location.marinePoints")} ({(draft.marineLocations ?? []).length})</h3>
          <div class="item-list">
            {#each draft.marineLocations ?? [] as ml (ml.id)}
              <div class="loc-row">
                <div class="loc-main">
                  <strong>{locDisplayName(ml)}</strong>
                  <span class="coord">{ml.latitude.toFixed(2)}, {ml.longitude.toFixed(2)}</span>
                  <span class="marine-badge" class:auto={ml.source === "auto"}>
                    {ml.source === "auto" ? t("location.marineSourceAuto") : t("location.marineSourceManual")}
                  </span>
                  <span class="marine-badge" class:climate={ml.category === "climate"} class:nearshore={ml.category === "nearshore"}>
                    {marineCategoryLabel(ml.category)}
                  </span>
                </div>
                <div class="row-actions">
                  {#if ml.isPrimary}
                    <span class="pill-primary">{t("app.primary")}</span>
                  {:else}
                    <button class="btn-sm" onclick={() => setPrimaryMarine(ml.id)}>{t("app.setPrimary")}</button>
                  {/if}
                  {#if (draft.marineLocations ?? []).length > 1}
                    <button class="btn-danger-sm" onclick={() => removeMarine(ml.id)}>{t("app.remove")}</button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        </section>

        <section class="group">
          <h3>{t("location.marinePresetsTitle")}</h3>
          <div class="preset-chips">
            {#each MARINE_PRESETS as p (p.id)}
              {@const exists = (draft.marineLocations ?? []).some((m) => m.id === p.id)}
              <button class="chip marine" class:added={exists} disabled={exists} onclick={() => addPresetMarine(p.id)}>
                {locDisplayName(p)} {exists ? "✓" : "+"}
              </button>
            {/each}
          </div>
        </section>

        <section class="group">
          <h3>{t("location.customAdd")}</h3>
          <p class="hint">{t("location.clickMapHint")}</p>
          <div class="custom-grid">
            <input type="text" bind:value={customMarineName} placeholder={t("location.nameLabel")} />
            <input type="number" step="0.01" bind:value={customMarineLat} placeholder={t("location.latLabel")} />
            <input type="number" step="0.01" bind:value={customMarineLon} placeholder={t("location.lonLabel")} />
            <button class="btn-sm" onclick={addCustomMarine}>{t("app.add")}</button>
          </div>
        </section>
      {:else if appState.settingsTab === "general"}
        <section class="group">
          <label class="field-row">
            <span>{t("settings.languageLabel")}</span>
            <select bind:value={draft.language}>
              <option value="auto">{t("settings.langAuto")}</option>
              <option value="ja">{t("settings.langJa")}</option>
              <option value="en">{t("settings.langEn")}</option>
            </select>
          </label>
        </section>

        <section class="group">
          <h3>{t("settings.intervalsTitle")}</h3>
          <div class="form-grid">
            <label>
              <span>{t("settings.weatherInterval")}</span>
              <input type="number" min="5" max="180" bind:value={draft.weatherIntervalMin} />
            </label>
            <label>
              <span>{t("settings.gridInterval")}</span>
              <input type="number" min="15" max="360" bind:value={draft.windGridIntervalMin} />
            </label>
            <label>
              <span>{t("settings.marineInterval")}</span>
              <input type="number" min="15" max="360" bind:value={draft.marineIntervalMin} />
            </label>
            <label>
              <span>{t("settings.lidenInterval")}</span>
              <input type="number" min="5" max="60" bind:value={draft.lidenIntervalMin} />
            </label>
            <label>
              <span>{t("settings.volcanoInterval")}</span>
              <input type="number" min="5" max="180" bind:value={draft.volcanoIntervalMin} />
            </label>
            <label>
              <span>{t("settings.spaceInterval")}</span>
              <input type="number" min="5" max="180" bind:value={draft.spaceIntervalMin} />
            </label>
          </div>
        </section>

        <section class="group">
          <h3>{t("settings.thresholdsTitle")}</h3>
          <div class="form-grid">
            <label>
              <span>{t("settings.lightningRadius")}</span>
              <input type="number" min="5" max="100" bind:value={draft.lightningRadiusKm} />
            </label>
            <label>
              <span>{t("settings.quakeMin")}</span>
              <select bind:value={draft.quakeMinIntensity}>
                <option value="1">1</option>
                <option value="2">2</option>
                <option value="3">3</option>
                <option value="4">4</option>
                <option value="5-">5-</option>
                <option value="5+">5+</option>
              </select>
            </label>
            <label>
              <span>{t("settings.pressureDrop")}</span>
              <input type="number" step="0.5" min="1" max="20" bind:value={draft.pressureDropHpa3h} />
            </label>
            <label>
              <span>{t("settings.kpThreshold")}</span>
              <input type="number" step="0.5" min="3" max="9" bind:value={draft.kpIndexThreshold} />
            </label>
            <label>
              <span>{t("settings.rainAlert")}</span>
              <input type="number" step="1" min="1" max="200" bind:value={draft.rainAlertMm} />
            </label>
            <label>
              <span>{t("settings.snowfallAlert")}</span>
              <input type="number" step="0.5" min="0.5" max="100" bind:value={draft.snowfallAlertCm} />
            </label>
            <label>
              <span>{t("settings.alertDistanceMode")}</span>
              <select bind:value={draft.alertDistanceMode}>
                <option value="nearest">{t("settings.alertDistanceNearest")}</option>
                <option value="primary">{t("settings.alertDistancePrimary")}</option>
              </select>
            </label>
            <label>
              <span>{t("settings.alertNearRadius")}</span>
              <input type="number" step="10" min="10" max="500" bind:value={draft.alertNearRadiusKm} />
            </label>
            <label>
              <span>{t("settings.alertMediumRadius")}</span>
              <input type="number" step="50" min="50" max="2000" bind:value={draft.alertMediumRadiusKm} />
            </label>
            <label>
              <span>{t("settings.eventWindowLabel")}</span>
              <select bind:value={draft.eventWindowHours}>
                <option value={24}>{t("events.window24h")}</option>
                <option value={72}>{t("events.window3d")}</option>
                <option value={168}>{t("events.window7d")}</option>
                <option value={720}>{t("events.window30d")}</option>
                <option value={-1}>{t("events.windowAll")}</option>
              </select>
            </label>
            {#if !appState.platform.isMobile}
              <label>
                <span>{t("settings.historyDaysLabel")}</span>
                <select bind:value={draft.historyDays}>
                  <option value={365}>{t("settings.historyRange1y")}</option>
                  <option value={1095}>{t("settings.historyRange3y")}</option>
                  <option value={1825}>{t("settings.historyRange5y")}</option>
                  <option value={3650}>{t("settings.historyRange10y")}</option>
                  <option value={10950}>{t("settings.historyRange30y")}</option>
                  <option value={-1}>{t("settings.historyRangeAll")}</option>
                </select>
              </label>
            {/if}
          </div>
          {#if !appState.platform.isMobile && (draft.historyDays === -1 || draft.historyDays >= 3650)}
            <p class="history-note">{t("settings.historyLongWarning")}</p>
          {/if}
          {#if !appState.platform.isMobile}
            <label class="check-row">
              <input
                type="checkbox"
                checked={!draft.disableHistorySync}
                onchange={(e) => {
                  if (draft) draft.disableHistorySync = !(e.currentTarget as HTMLInputElement).checked;
                }}
              />
              <span>{t("settings.historyAutoSync")}</span>
            </label>
          {/if}
          <label class="check-row">
            <input type="checkbox" bind:checked={draft.mobileLightMode} />
            <span>{t("settings.mobileLightMode")}</span>
          </label>

          {#if !appState.platform.isMobile}
            <div class="history-box">
            <div class="history-head">
              <h4>{t("settings.historyStatusTitle")}</h4>
              <button class="mini-btn" type="button" disabled={syncingNow} onclick={handleSyncNow}>
                {syncingNow ? "..." : t("settings.historySyncNow")}
              </button>
            </div>
            {#if appState.historySyncStatuses.length === 0}
              <p class="muted-small">{t("settings.historyNoSyncStatus")}</p>
            {:else}
              <div class="sync-list">
                {#each appState.historySyncStatuses as st (`${st.kind}:${st.locationId}`)}
                  <div class="sync-row">
                    <div class="sync-meta">
                      <span class="sync-name">[{st.kind}] {st.locationName || st.locationId}</span>
                      <span class="sync-range">
                        {st.earliestDate || "-"} 〜 {st.latestDate || "-"} ({st.storedCount} {t("settings.historyStoredCount")})
                      </span>
                    </div>
                    <div class="sync-right">
                      <span
                        class="sync-badge"
                        class:done={st.state === "done"}
                        class:warn={st.state === "rate_limited"}
                        class:err={st.state === "error"}
                        title={st.lastError || ""}
                      >
                        {#if st.state === "done"}
                          {t("charts.syncDone")}
                        {:else if st.state === "rate_limited"}
                          {t("charts.syncRateLimited")}
                        {:else if st.state === "error"}
                          Error
                        {:else}
                          {st.progressPct.toFixed(0)}%
                        {/if}
                      </span>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}

            <div class="clear-row">
              <span class="clear-title">{t("settings.historyClearTitle")}</span>
              <div class="clear-controls">
                <select bind:value={clearScope}>
                  <option value="all">{t("settings.historyClearScopeAll")}</option>
                  <option value="weather">{t("settings.historyClearScopeWeather")}</option>
                  <option value="marine">{t("settings.historyClearScopeMarine")}</option>
                  <option value="quake">{t("settings.historyClearScopeQuake")}</option>
                </select>
                {#if !clearConfirmOpen}
                  <button class="danger-btn" type="button" onclick={() => (clearConfirmOpen = true)}>
                    {t("settings.historyClearBtn")}
                  </button>
                {/if}
              </div>
            </div>
            {#if clearConfirmOpen}
              <div class="clear-confirm">
                <span>{t("settings.historyClearConfirm")}</span>
                <div class="clear-confirm-actions">
                  <button class="danger-btn" type="button" disabled={clearingHistory} onclick={() => handleClearHistory(false)}>
                    {t("settings.historyClearBtn")}
                  </button>
                  <button class="mini-btn" type="button" disabled={clearingHistory} onclick={() => handleClearHistory(true)}>
                    {t("settings.historyClearResyncBtn")}
                  </button>
                  <button class="mini-btn" type="button" disabled={clearingHistory} onclick={() => (clearConfirmOpen = false)}>
                    {t("app.cancel")}
                  </button>
                </div>
              </div>
            {/if}
            {#if clearFeedback}
              <p class="clear-feedback">{clearFeedback}</p>
            {/if}
            </div>
          {/if}
        </section>
      {:else if appState.settingsTab === "ai"}
        <section class="group">
          <label class="field-row">
            <span>{t("ai.providerLabel")}</span>
            <select
              bind:value={draft.llmProvider}
              onchange={() => {
                if (draft?.llmProvider === "tensai") {
                  void loadLocalAIInfo();
                }
              }}
            >
              <option value="none">{t("ai.providerNone")}</option>
              <option value="tensai">{t("ai.providerTensai")}</option>
              <option value="ollama">{t("ai.providerOllama")}</option>
              <option value="gemini">{t("ai.providerGemini")}</option>
              <option value="openai">{t("ai.providerOpenAI")}</option>
              <option value="anthropic">{t("ai.providerAnthropic")}</option>
            </select>
          </label>

          {#if appState.platform.isMobile && draft.llmProvider === "tensai"}
            <p class="hint">{t("ai.mobileNotSupported")}</p>
          {/if}

          {#if draft.llmProvider === "tensai"}
            <div class="field-row">
              <span>{t("ai.localModelLabel")}</span>
              {#if localModels.length > 0}
                <select bind:value={draft.llmModel}>
                  {#each localModels as m (m.path)}
                    <option value={m.name}>{m.name} ({m.size_human})</option>
                  {/each}
                </select>
              {:else}
                <button class="btn-primary-sm" type="button" onclick={() => (showModelManager = true)}>
                  {t("ai.noLocalModel")}
                </button>
              {/if}
            </div>
            <div class="row-actions">
              <button class="btn-sm" type="button" onclick={() => (showModelManager = true)}>
                {t("ai.manageModelsBtn")}
              </button>
            </div>
            {#if hardwareStatus}
              <div class="history-box">
                <div class="history-head">
                  <h4>{t("ai.accelerationLabel")}</h4>
                  <span class="sync-badge done">{hardwareStatus.acceleration || "CPU"}</span>
                </div>
                <p class="muted-small">{hardwareStatus.detail}</p>
              </div>
            {/if}
          {:else if draft.llmProvider && draft.llmProvider !== "none"}
            <div class="form-grid">
              <label>
                <span>{t("ai.modelLabel")}</span>
                <input
                  type="text"
                  bind:value={draft.llmModel}
                  placeholder={draft.llmProvider === "ollama"
                    ? "llama3.2"
                    : draft.llmProvider === "gemini"
                      ? "gemini-2.5-flash"
                      : draft.llmProvider === "openai"
                        ? "gpt-4o-mini"
                        : "claude-3-5-sonnet-latest"}
                />
              </label>
              <label>
                <span>{t("ai.baseUrlLabel")}</span>
                <input
                  type="text"
                  bind:value={draft.llmBaseUrl}
                  placeholder={draft.llmProvider === "ollama" ? "http://localhost:11434" : ""}
                />
              </label>
            </div>
            {#if draft.llmProvider !== "ollama"}
              <label class="field-row">
                <span>{t("ai.apiKeyLabel")}</span>
                <input type="password" bind:value={draft.llmApiKey} placeholder="sk-... / AIza..." />
              </label>
            {/if}
          {/if}

          {#if !appState.platform.isMobile}
            <div class="history-box">
              <div class="history-head">
                <h4>{t("settings.mcpTitle")}</h4>
                <span
                  class="sync-badge"
                  class:done={appState.mcp.running}
                  class:err={Boolean(appState.mcp.error)}
                >
                  {appState.mcp.running ? t("settings.mcpStatusRunning") : t("settings.mcpStatusStopped")}
                </span>
              </div>
              <label class="check-row">
                <input type="checkbox" bind:checked={draft.mcpEnabled} />
                <span>{t("settings.mcpEnable")}</span>
              </label>
              <div class="form-grid">
                <label>
                  <span>{t("settings.mcpPortLabel")}</span>
                  <input type="number" min="1024" max="65535" bind:value={draft.mcpPort} />
                </label>
              </div>
              {#if appState.mcp.running && appState.mcp.endpoint}
                <div class="mcp-endpoint">
                  <span class="muted-small">{t("settings.mcpEndpointLabel")}:</span>
                  <code>{appState.mcp.endpoint}</code>
                </div>
              {/if}
              {#if appState.mcp.error}
                <div class="u-err">{appState.mcp.error}</div>
              {/if}
              <p class="muted-small">{t("settings.mcpLocalOnlyNote")}</p>
            </div>
          {/if}
        </section>
      {:else if appState.settingsTab === "update" && appState.platform.canSelfUpdate}
        <section class="group">
          <div class="update-info">
            <div>{t("settings.currentVersion")}: <strong>{appState.updater.currentVersion || "dev"}</strong></div>
            {#if appState.updater.latestVersion}
              <div>{t("settings.latestVersion")}: <strong>{appState.updater.latestVersion}</strong></div>
            {/if}
            <div class="u-status">State: {appState.updater.state}</div>
            {#if appState.updater.error}
              <div class="u-err">{appState.updater.error}</div>
            {/if}
          </div>

          <label class="check-row">
            <input type="checkbox" bind:checked={draft.autoCheckUpdate} />
            <span>{t("settings.autoCheckUpdate")}</span>
          </label>

          <div class="update-actions">
            <button class="btn-sm" onclick={() => checkForUpdate()} disabled={appState.updater.state === "checking"}>
              {appState.updater.state === "checking" ? t("settings.checkingUpdate") : t("settings.checkUpdateNow")}
            </button>
            {#if appState.updater.state === "available"}
              <button class="btn-primary-sm" onclick={() => installUpdate()}>
                {t("settings.downloadInstall")}
              </button>
            {:else if appState.updater.state === "downloading"}
              <span>{appState.downloadProgress}%</span>
            {:else if appState.updater.state === "ready"}
              <button class="btn-primary-sm" onclick={() => restartApp()}>
                {t("settings.restartNow")}
              </button>
            {/if}
          </div>
        </section>
      {/if}
    </div>
  {/if}

  <div class="settings-footer">
    {#if !inline && onClose}
      <button class="btn-sm" onclick={onClose}>{t("app.cancel")}</button>
    {/if}
    <button class="btn-save" onclick={handleSave} disabled={saving || !draft}>
      {t("app.save")}
    </button>
  </div>
</div>

{#if showModelManager}
  <div class="submodal-backdrop" role="dialog" aria-modal="true">
    <div class="submodal-wrap">
      <ModelManagerModal
        onClose={() => {
          showModelManager = false;
          void loadLocalAIInfo();
        }}
        onModelsChanged={() => void loadLocalAIInfo()}
      />
    </div>
  </div>
{/if}

<style>
  .settings-card {
    background: #0d1222;
    border: 1px solid rgba(56, 189, 248, 0.25);
    border-radius: 12px;
    width: min(720px, 94vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    color: #e2e8f0;
  }
  .settings-card.inline {
    width: 100%;
    max-height: 100%;
    height: 100%;
    border: none;
    border-radius: 0;
  }
  .settings-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.15);
  }
  h2 {
    margin: 0;
    font-size: 1rem;
  }
  .close-btn {
    background: transparent;
    border: none;
    color: #94a3b8;
    font-size: 1.1rem;
    cursor: pointer;
  }
  .tab-bar {
    display: flex;
    gap: 4px;
    padding: 8px 14px 0;
    border-bottom: 1px solid rgba(148, 163, 184, 0.15);
    overflow-x: auto;
  }
  .tab-bar button {
    position: relative;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: #94a3b8;
    padding: 7px 12px;
    font-size: 0.8rem;
    cursor: pointer;
    white-space: nowrap;
  }
  .tab-bar button.active {
    color: #38bdf8;
    border-bottom-color: #38bdf8;
    font-weight: 600;
  }
  .mini-dot {
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f43f5e;
    margin-left: 5px;
  }
  .settings-body {
    flex: 1;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .group h3 {
    margin: 0;
    font-size: 0.82rem;
    color: #94a3b8;
  }
  .group-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .candidate-box {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border-radius: 8px;
    background: rgba(14, 116, 144, 0.18);
    border: 1px solid rgba(56, 189, 248, 0.35);
    font-size: 0.8rem;
  }
  .hint {
    margin: 0;
    font-size: 0.75rem;
    color: #64748b;
  }
  .item-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .loc-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: rgba(15, 23, 42, 0.7);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 8px;
    font-size: 0.8rem;
  }
  .loc-main {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .marine-badge {
    font-size: 0.65rem;
    padding: 1px 6px;
    border-radius: 999px;
    background: rgba(148, 163, 184, 0.16);
    color: #94a3b8;
    border: 1px solid rgba(148, 163, 184, 0.25);
  }
  .marine-badge.auto {
    background: rgba(56, 189, 248, 0.14);
    color: #7dd3fc;
    border-color: rgba(56, 189, 248, 0.35);
  }
  .marine-badge.nearshore {
    background: rgba(45, 212, 191, 0.14);
    color: #5eead4;
    border-color: rgba(45, 212, 191, 0.35);
  }
  .marine-badge.climate {
    background: rgba(245, 158, 11, 0.14);
    color: #fcd34d;
    border-color: rgba(245, 158, 11, 0.35);
  }
  .coord {
    margin-left: 8px;
    font-size: 0.72rem;
    color: #64748b;
  }
  .row-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .pill-primary {
    font-size: 0.68rem;
    padding: 2px 7px;
    border-radius: 999px;
    background: rgba(56, 189, 248, 0.2);
    color: #38bdf8;
  }
  .preset-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chip {
    background: rgba(30, 41, 59, 0.8);
    border: 1px solid rgba(148, 163, 184, 0.25);
    color: #cbd5e1;
    border-radius: 999px;
    padding: 4px 10px;
    font-size: 0.74rem;
    cursor: pointer;
  }
  .chip.added {
    border-color: rgba(56, 189, 248, 0.5);
    color: #38bdf8;
    opacity: 0.7;
    cursor: default;
  }
  .chip.marine.added {
    border-color: rgba(45, 212, 191, 0.5);
    color: #2dd4bf;
  }
  .search-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .search-row input {
    flex: 1;
    min-width: 0;
  }
  .search-results {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .result-item {
    display: flex;
    justify-content: space-between;
    padding: 6px 10px;
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.8);
    border: 1px solid rgba(148, 163, 184, 0.2);
    color: #e2e8f0;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .custom-grid {
    display: grid;
    grid-template-columns: 1.5fr 1fr 1fr auto;
    gap: 6px;
  }
  .form-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 10px;
  }
  .form-grid label,
  .field-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 0.76rem;
    color: #cbd5e1;
  }
  input[type="text"],
  input[type="number"],
  select {
    background: #0f172a;
    border: 1px solid rgba(148, 163, 184, 0.28);
    border-radius: 6px;
    color: #f8fafc;
    padding: 6px 8px;
    font-size: 0.8rem;
    width: 100%;
    box-sizing: border-box;
  }
  .check-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.8rem;
    color: #cbd5e1;
    margin-top: 6px;
  }
  .btn-sm,
  .btn-primary-sm,
  .btn-danger-sm,
  .btn-save {
    border-radius: 6px;
    padding: 5px 10px;
    font-size: 0.76rem;
    cursor: pointer;
    border: 1px solid rgba(148, 163, 184, 0.3);
    background: #1e293b;
    color: #e2e8f0;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .btn-primary-sm,
  .btn-save {
    background: #0284c7;
    border-color: #38bdf8;
    color: #ffffff;
    font-weight: 600;
  }
  .btn-danger-sm {
    border-color: rgba(244, 63, 94, 0.45);
    color: #fda4af;
  }
  .update-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 0.82rem;
  }
  .u-err {
    color: #fb7185;
  }
  .update-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .settings-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 10px 16px;
    border-top: 1px solid rgba(148, 163, 184, 0.15);
  }
  .history-note {
    margin: 4px 0 8px;
    font-size: 0.75rem;
    color: #fcd34d;
    background: rgba(251, 191, 36, 0.1);
    border: 1px solid rgba(251, 191, 36, 0.28);
    border-radius: 6px;
    padding: 6px 8px;
  }
  .history-box {
    margin-top: 10px;
    padding: 10px;
    border-radius: 8px;
    background: rgba(15, 23, 42, 0.55);
    border: 1px solid rgba(148, 163, 184, 0.18);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .history-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }
  .history-head h4 {
    margin: 0;
    font-size: 0.82rem;
    color: #e2e8f0;
  }
  .sync-list {
    display: flex;
    flex-direction: column;
    gap: 5px;
    max-height: 180px;
    overflow-y: auto;
  }
  .sync-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.72);
    border: 1px solid rgba(148, 163, 184, 0.12);
    font-size: 0.74rem;
  }
  .sync-meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .sync-name {
    color: #f1f5f9;
    font-weight: 600;
  }
  .sync-range {
    color: #94a3b8;
    font-size: 0.68rem;
  }
  .sync-badge {
    padding: 2px 7px;
    border-radius: 999px;
    font-size: 0.68rem;
    background: rgba(56, 189, 248, 0.16);
    color: #7dd3fc;
    white-space: nowrap;
  }
  .sync-badge.done {
    background: rgba(16, 185, 129, 0.16);
    color: #34d399;
  }
  .sync-badge.warn {
    background: rgba(251, 191, 36, 0.16);
    color: #fcd34d;
  }
  .sync-badge.err {
    background: rgba(244, 63, 94, 0.16);
    color: #fda4af;
  }
  .clear-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding-top: 6px;
    border-top: 1px solid rgba(148, 163, 184, 0.14);
  }
  .clear-title {
    font-size: 0.78rem;
    color: #cbd5e1;
  }
  .clear-controls {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .clear-controls select {
    width: auto;
    background: rgba(15, 23, 42, 0.85);
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    color: #e2e8f0;
    font-size: 0.74rem;
    padding: 4px 8px;
  }
  .clear-confirm {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px;
    border-radius: 6px;
    background: rgba(244, 63, 94, 0.1);
    border: 1px solid rgba(244, 63, 94, 0.3);
    font-size: 0.74rem;
    color: #fecdd3;
  }
  .clear-confirm-actions {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .clear-feedback {
    margin: 0;
    font-size: 0.74rem;
    color: #34d399;
  }
  .mini-btn,
  .danger-btn {
    padding: 4px 10px;
    border-radius: 6px;
    font-size: 0.72rem;
    cursor: pointer;
    border: 1px solid rgba(148, 163, 184, 0.3);
    background: #1e293b;
    color: #e2e8f0;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .danger-btn {
    border-color: rgba(244, 63, 94, 0.5);
    background: rgba(159, 18, 57, 0.35);
    color: #fecdd3;
  }
  .muted-small {
    margin: 0;
    font-size: 0.74rem;
    color: #64748b;
  }
  .mcp-endpoint {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .mcp-endpoint code {
    font-size: 0.74rem;
    padding: 2px 6px;
    border-radius: 4px;
    background: rgba(15, 23, 42, 0.9);
    border: 1px solid rgba(56, 189, 248, 0.3);
    color: #7dd3fc;
    user-select: all;
  }
  .submodal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(2, 6, 23, 0.8);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
    z-index: 60;
  }
  .submodal-wrap {
    width: min(680px, 100%);
  }
</style>
