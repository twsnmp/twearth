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

  $effect(() => {
    if (appState.config && !draft) {
      draft = structuredClone($state.snapshot(appState.config));
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
        isPrimary: list.length === 0,
        order: list.length,
      } satisfies MarineLocation,
    ];
    customMarineName = "";
  }

  async function handleSave() {
    if (!draft) return;
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
          <h3>{t("location.marinePoints")} ({(draft.marineLocations ?? []).length})</h3>
          <div class="item-list">
            {#each draft.marineLocations ?? [] as ml (ml.id)}
              <div class="loc-row">
                <div>
                  <strong>{locDisplayName(ml)}</strong>
                  <span class="coord">{ml.latitude.toFixed(2)}, {ml.longitude.toFixed(2)}</span>
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
              <span>{t("settings.eventWindowLabel")}</span>
              <select bind:value={draft.eventWindowHours}>
                <option value={24}>{t("events.window24h")}</option>
                <option value={72}>{t("events.window3d")}</option>
                <option value={168}>{t("events.window7d")}</option>
                <option value={720}>{t("events.window30d")}</option>
                <option value={-1}>{t("events.windowAll")}</option>
              </select>
            </label>
          </div>
          <label class="check-row">
            <input type="checkbox" bind:checked={draft.mobileLightMode} />
            <span>{t("settings.mobileLightMode")}</span>
          </label>
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
</style>
