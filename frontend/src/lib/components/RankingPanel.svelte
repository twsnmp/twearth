<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type {
    RankingCatalog,
    RankingFactorDescriptor,
    RankingProgress,
    RankingResultItem,
  } from "../../../bindings/github.com/twsnmp/twearth/internal/analysis/models";
  import type { CitySearchResult } from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";
  import type { MunicipalityEntry } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
  import {
    appState,
    focusMapAt,
    openViewAI,
    saveAppConfig,
    searchCities,
  } from "../stores/appState.svelte";
  import { i18nState, locDisplayName, t } from "../i18n/index.svelte";

  type SortMode = "total" | "quake" | "disaster" | "climate" | "lore" | "volcano";
  type TableSortColumn =
    | "rank"
    | "total"
    | "municipality"
    | "meanTemp"
    | "quakeCount"
    | "disasterCount"
    | "volcanoDist"
    | "factor_quake"
    | "factor_disaster"
    | "factor_climate"
    | "factor_lore"
    | "factor_volcano";
  type SortDirection = "asc" | "desc";

  let catalog = $state<RankingCatalog>({
    factors: [],
    regions: [],
    prefectures: [],
    prefecturesEn: [],
    totalJapanCount: 1911,
    totalGlobalCount: 122,
    totalCustomCount: 0,
    customLocations: [],
  });

  let progress = $state<RankingProgress>({
    status: "idle",
    config: {
      scope: "japan_all",
      region: "all",
      prefecture: "all",
      rangeKey: "5y",
      weights: {
        climate: 25,
        quake: 35,
        disaster: 20,
        lore: 10,
        volcano: 10,
      },
      idealTempC: 16,
      penalizeExtremeHeat: true,
      penalizeExtremeCold: true,
      penalizeHeavySnow: true,
      quakeRadiusKm: 75,
      disasterRadiusKm: 35,
      loreRadiusKm: 15,
      fetchMissingClimate: false,
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

  let items = $state<RankingResultItem[]>([]);
  let selectedItem = $state<RankingResultItem | null>(null);
  let errorMsg = $state("");
  let confirmClear = $state(false);

  // Left sidebar controls
  let scope = $state("japan_all");
  let region = $state("all");
  let prefecture = $state("all");
  let rangeKey = $state("5y");
  let weights = $state<Record<string, number>>({
    climate: 25,
    quake: 35,
    disaster: 20,
    lore: 10,
    volcano: 10,
  });
  let idealTempC = $state(16.0);
  let penalizeExtremeHeat = $state(true);
  let penalizeExtremeCold = $state(true);
  let penalizeHeavySnow = $state(true);
  let quakeRadiusKm = $state(75);
  let disasterRadiusKm = $state(35);
  let loreRadiusKm = $state(15);
  let fetchMissingClimate = $state(false);
  let forceRerun = $state(false);

  // Custom municipality registration controls
  let customModalOpen = $state(false);
  let cityQuery = $state("");
  let searchingCities = $state(false);
  let citySearchResults = $state<CitySearchResult[]>([]);
  let customName = $state("");
  let customPref = $state("");
  let customGroup = $state<"japan" | "global">("japan");
  let customLat = $state(35.6895);
  let customLon = $state(139.6917);

  // Right toolbar filters & sorting
  let searchText = $state("");
  let minScore = $state(0);
  let sortBy = $state<SortMode>("total");
  let sortCol = $state<TableSortColumn>("total");
  let sortDir = $state<SortDirection>("desc");
  let sortStatusMsg = $state("");

  const nameCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

  const factorList = $derived<RankingFactorDescriptor[]>(
    catalog.factors && catalog.factors.length > 0
      ? catalog.factors
      : [
          { id: "climate", defaultWeight: 25 },
          { id: "quake", defaultWeight: 35 },
          { id: "disaster", defaultWeight: 20 },
          { id: "lore", defaultWeight: 10, japanOnly: true },
          { id: "volcano", defaultWeight: 10 },
        ]
  );

  const isRunning = $derived(progress.status === "running");
  const isPaused = $derived(progress.status === "paused");
  const progressPct = $derived(
    progress.total > 0 ? Math.min(100, Math.round((progress.completed / progress.total) * 100)) : 0
  );

  const observedCoordSet = $derived.by(() => {
    const s = new Set<string>();
    for (const loc of appState.config?.locations ?? []) {
      s.add(loc.name);
      s.add(`${loc.latitude.toFixed(2)},${loc.longitude.toFixed(2)}`);
    }
    return s;
  });

  const filteredItems = $derived.by(() => {
    const q = searchText.trim().toLowerCase();
    let list = items.filter((it) => {
      if (it.totalScore < minScore) return false;
      if (q) {
        const m = it.municipality;
        const hay = `${m.name} ${m.nameEn} ${m.prefecture ?? ""} ${m.prefectureEn ?? ""} ${m.country ?? ""} ${m.countryEn ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });

    list = [...list].sort((a, b) => {
      const dir = sortDir === "asc" ? 1 : -1;
      let cmp = 0;
      switch (sortCol) {
        case "rank": {
          const ra = a.rank > 0 ? a.rank : Number.MAX_SAFE_INTEGER;
          const rb = b.rank > 0 ? b.rank : Number.MAX_SAFE_INTEGER;
          cmp = ra - rb;
          break;
        }
        case "total":
          cmp = a.totalScore - b.totalScore;
          break;
        case "municipality":
          cmp = nameCollator.compare(formatMuniName(a.municipality), formatMuniName(b.municipality));
          break;
        case "meanTemp":
          cmp = a.meanTempC - b.meanTempC;
          break;
        case "quakeCount":
          cmp = a.quakeCount - b.quakeCount;
          if (cmp === 0) cmp = a.quakeMaxMag - b.quakeMaxMag;
          break;
        case "disasterCount":
          cmp = a.disasterCount - b.disasterCount;
          if (cmp === 0) cmp = a.loreCount - b.loreCount;
          break;
        case "volcanoDist": {
          const va = a.volcanoDistKm > 0 && a.volcanoDistKm < 9000 ? a.volcanoDistKm : Number.POSITIVE_INFINITY;
          const vb = b.volcanoDistKm > 0 && b.volcanoDistKm < 9000 ? b.volcanoDistKm : Number.POSITIVE_INFINITY;
          if (va !== vb) {
            if (va === Number.POSITIVE_INFINITY) return 1;
            if (vb === Number.POSITIVE_INFINITY) return -1;
            cmp = va - vb;
          }
          break;
        }
        default: {
          const factorKey = sortCol.replace("factor_", "");
          const fa = a.factorScores?.[factorKey]?.score ?? 0;
          const fb = b.factorScores?.[factorKey]?.score ?? 0;
          cmp = fa - fb;
          break;
        }
      }
      if (cmp !== 0) return cmp * dir;
      if (b.totalScore !== a.totalScore) return b.totalScore - a.totalScore;
      return a.municipality.id.localeCompare(b.municipality.id);
    });

    return list;
  });

  function toggleHeaderSort(col: TableSortColumn, label: string) {
    if (sortCol === col) {
      sortDir = sortDir === "asc" ? "desc" : "asc";
    } else {
      sortCol = col;
      sortDir = "asc";
    }
    if (col === "total") {
      sortBy = "total";
    } else if (col.startsWith("factor_")) {
      sortBy = col.replace("factor_", "") as SortMode;
    }
    sortStatusMsg = `Sorted by ${label}, ${sortDir === "asc" ? "ascending" : "descending"}, ${filteredItems.length} rows`;
  }

  function handleToolbarSortChange(mode: SortMode) {
    sortBy = mode;
    sortCol = mode === "total" ? "total" : (`factor_${mode}` as TableSortColumn);
    sortDir = "desc";
  }

  function factorLabel(factorId: string): string {
    const key = `ranking.factors.${factorId}`;
    const val = t(key);
    return val === key ? factorId : val;
  }

  function regionLabel(reg: string): string {
    const key = `ranking.regions.${reg}`;
    const val = t(key);
    return val === key ? reg : val;
  }

  function formatMuniName(m: MunicipalityEntry): string {
    return locDisplayName(m);
  }

  function formatMuniSub(m: MunicipalityEntry): string {
    if (i18nState.lang === "en") {
      return m.prefectureEn || m.countryEn || m.prefecture || "";
    }
    return m.prefecture || m.country || "";
  }

  function isWatched(m: MunicipalityEntry): boolean {
    return (
      observedCoordSet.has(m.name) ||
      observedCoordSet.has(`${m.latitude.toFixed(2)},${m.longitude.toFixed(2)}`)
    );
  }

  function scoreClass(score: number): string {
    if (score >= 80) return "score-high";
    if (score >= 65) return "score-mid";
    return "score-low";
  }

  function rankClass(rank: number): string {
    if (rank === 1) return "rank-1";
    if (rank === 2) return "rank-2";
    if (rank === 3) return "rank-3";
    return "";
  }

  function statusBadgeLabel(st: string): string {
    switch (st) {
      case "running":
        return t("ranking.statusRunning");
      case "paused":
        return t("ranking.statusPaused");
      case "completed":
        return t("ranking.statusCompleted");
      case "error":
        return t("ranking.statusError");
      default:
        return t("ranking.statusIdle");
    }
  }

  function applyWeightPreset(preset: "balanced" | "safety" | "comfort" | "snowfree") {
    const next: Record<string, number> = { ...weights };
    for (const f of factorList) {
      next[f.id] = f.defaultWeight;
    }
    if (preset === "safety") {
      next.quake = 50;
      next.disaster = 35;
      next.lore = 25;
      next.volcano = 20;
      next.climate = 10;
    } else if (preset === "comfort") {
      next.climate = 55;
      next.quake = 25;
      next.disaster = 15;
      next.lore = 5;
      next.volcano = 5;
    } else if (preset === "snowfree") {
      next.climate = 50;
      next.quake = 25;
      next.disaster = 15;
      next.lore = 5;
      next.volcano = 5;
      idealTempC = 17.5;
      penalizeHeavySnow = true;
      penalizeExtremeCold = true;
    }
    weights = next;
  }

  function applyConfigFromProgress(prog: RankingProgress) {
    if (!prog?.config) return;
    const c = prog.config;
    if (c.scope) scope = c.scope;
    if (c.region) region = c.region;
    if (c.prefecture) prefecture = c.prefecture;
    if (c.rangeKey) rangeKey = c.rangeKey;
    if (c.weights) {
      const merged: Record<string, number> = { ...weights };
      for (const [k, v] of Object.entries(c.weights)) {
        if (typeof v === "number") merged[k] = v;
      }
      weights = merged;
    }
    if (c.idealTempC) idealTempC = c.idealTempC;
    penalizeExtremeHeat = c.penalizeExtremeHeat ?? true;
    penalizeExtremeCold = c.penalizeExtremeCold ?? true;
    penalizeHeavySnow = c.penalizeHeavySnow ?? true;
    if (c.quakeRadiusKm > 0) quakeRadiusKm = c.quakeRadiusKm;
    if (c.disasterRadiusKm > 0) disasterRadiusKm = c.disasterRadiusKm;
    if (c.loreRadiusKm > 0) loreRadiusKm = c.loreRadiusKm;
    fetchMissingClimate = !!c.fetchMissingClimate;
    forceRerun = !!c.forceRerun;
  }

  async function startRanking() {
    errorMsg = "";
    items = [];
    selectedItem = null;
    try {
      await AppService.StartMunicipalityRanking({
        scope,
        region: region === "all" ? "" : region,
        prefecture: prefecture === "all" ? "" : prefecture,
        rangeKey,
        weights,
        idealTempC: Number(idealTempC),
        penalizeExtremeHeat,
        penalizeExtremeCold,
        penalizeHeavySnow,
        quakeRadiusKm: Number(quakeRadiusKm),
        disasterRadiusKm: Number(disasterRadiusKm),
        loreRadiusKm: Number(loreRadiusKm),
        fetchMissingClimate,
        forceRerun,
      });
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function pauseRanking() {
    errorMsg = "";
    try {
      await AppService.PauseMunicipalityRanking();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function resumeRanking() {
    errorMsg = "";
    try {
      await AppService.ResumeMunicipalityRanking();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function clearRankingCache() {
    errorMsg = "";
    confirmClear = false;
    try {
      await AppService.ClearMunicipalityRankingCache(false);
      items = [];
      selectedItem = null;
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function handleCitySearch() {
    if (!cityQuery.trim()) return;
    searchingCities = true;
    try {
      citySearchResults = await searchCities(cityQuery);
    } finally {
      searchingCities = false;
    }
  }

  async function addSearchedMunicipality(r: CitySearchResult) {
    errorMsg = "";
    const isJp = r.country_code?.toUpperCase() === "JP" || r.country === "日本" || r.country === "Japan";
    try {
      await AppService.AddCustomMunicipality({
        id: `custom-geo-${r.id}`,
        name: r.name,
        nameEn: r.name,
        prefecture: r.admin1 || r.country,
        prefectureEn: r.admin1 || r.country,
        country: r.country,
        countryEn: r.country,
        region: "custom",
        group: isJp ? "japan" : "global",
        latitude: Number(r.latitude.toFixed(4)),
        longitude: Number(r.longitude.toFixed(4)),
        source: "custom",
      });
      catalog = await AppService.GetMunicipalityRankingCatalog();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function addManualMunicipality() {
    if (!customName.trim()) return;
    errorMsg = "";
    try {
      await AppService.AddCustomMunicipality({
        id: "",
        name: customName.trim(),
        nameEn: customName.trim(),
        prefecture: customPref.trim(),
        prefectureEn: customPref.trim(),
        country: customGroup === "japan" ? "日本" : customPref.trim() || "World",
        countryEn: customGroup === "japan" ? "Japan" : customPref.trim() || "World",
        region: "custom",
        group: customGroup,
        latitude: Number(customLat),
        longitude: Number(customLon),
        source: "custom",
      });
      customName = "";
      catalog = await AppService.GetMunicipalityRankingCatalog();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function removeCustomMunicipality(id: string) {
    errorMsg = "";
    try {
      await AppService.RemoveCustomMunicipality(id);
      catalog = await AppService.GetMunicipalityRankingCatalog();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function addMunicipalityToWatched(m: MunicipalityEntry) {
    if (!appState.config || isWatched(m)) return;
    const list = appState.config.locations ?? [];
    const label = m.prefecture && m.prefecture !== m.name ? `${m.name} (${m.prefecture})` : m.name;
    const labelEn = m.prefectureEn && m.prefectureEn !== m.nameEn ? `${m.nameEn} (${m.prefectureEn})` : m.nameEn;
    await saveAppConfig({
      ...appState.config,
      locations: [
        ...list,
        {
          id: `rank-${m.id}`,
          name: label,
          nameEn: labelEn || label,
          latitude: m.latitude,
          longitude: m.longitude,
          source: "custom",
          isPrimary: list.length === 0,
          order: list.length,
        },
      ],
    });
  }

  function explainMunicipalityWithAI(item: RankingResultItem) {
    const m = item.municipality;
    const summaryParts: string[] = [
      `${m.prefecture ?? ""} ${m.name} (${m.nameEn}) [${m.latitude.toFixed(3)}, ${m.longitude.toFixed(3)}]`,
      `Rank #${item.rank} / Total Safety & Livability Score: ${item.totalScore.toFixed(1)}/100`,
    ];
    for (const f of factorList) {
      const fs = item.factorScores?.[f.id];
      if (fs && fs.applicable) {
        summaryParts.push(`${f.id}: ${fs.score.toFixed(1)} (${fs.summary})`);
      }
    }
    openViewAI({
      view: "ranking",
      mode: "single",
      metric: summaryParts.join(" | "),
      rangeKey,
      lang: i18nState.lang,
    });
  }

  function explainRankingViewWithAI() {
    const top5 = filteredItems
      .slice(0, 5)
      .map((it, idx) => `#${it.rank > 0 ? it.rank : idx + 1} ${it.municipality.prefecture ?? ""}${it.municipality.name} (${it.totalScore.toFixed(1)})`)
      .join(", ");
    openViewAI({
      view: "ranking",
      mode: scope,
      metric: `Scope=${scope}, Sort=${sortCol}:${sortDir}, Top5=[${top5}]`,
      rangeKey,
      lang: i18nState.lang,
    });
  }

  function mergeBatchAndAssignRanks(current: RankingResultItem[], incoming: RankingResultItem[]): RankingResultItem[] {
    const byId = new Map<string, RankingResultItem>();
    for (const it of current) {
      byId.set(it.municipality.id, it);
    }
    for (const it of incoming) {
      if (it?.municipality?.id) {
        byId.set(it.municipality.id, it);
      }
    }
    const merged = Array.from(byId.values());
    merged.sort((a, b) => {
      if (b.totalScore !== a.totalScore) return b.totalScore - a.totalScore;
      return a.municipality.id.localeCompare(b.municipality.id);
    });
    for (let i = 0; i < merged.length; i++) {
      merged[i].rank = i + 1;
    }
    return merged;
  }

  onMount(() => {
    let disposed = false;
    (async () => {
      try {
        const [cat, prog, cached] = await Promise.all([
          AppService.GetMunicipalityRankingCatalog(),
          AppService.GetMunicipalityRankingStatus(),
          AppService.ListMunicipalityRankingResults(),
        ]);
        if (disposed) return;
        catalog = cat;
        const initWeights: Record<string, number> = { ...weights };
        for (const f of cat.factors ?? []) {
          if (initWeights[f.id] === undefined) {
            initWeights[f.id] = f.defaultWeight;
          }
        }
        weights = initWeights;
        progress = prog;
        applyConfigFromProgress(prog);
        items = mergeBatchAndAssignRanks([], cached ?? []);
      } catch (e) {
        errorMsg = e instanceof Error ? e.message : String(e);
      }
    })();

    const offProg = Events.On("twearth:ranking-progress", (ev) => {
      const p = ev?.data as RankingProgress | undefined;
      if (p) {
        progress = p;
      }
    });

    const offBatch = Events.On("twearth:ranking-results-batch", (ev) => {
      const batch = ev?.data as RankingResultItem[] | undefined;
      if (!Array.isArray(batch) || batch.length === 0) return;
      items = mergeBatchAndAssignRanks(items, batch);
    });

    return () => {
      disposed = true;
      offProg?.();
      offBatch?.();
    };
  });
</script>

<div class="rank-root">
  <aside class="rank-sidebar">
    <div class="sidebar-header">
      <h2>{t("ranking.title")}</h2>
      <p class="subtitle">{t("ranking.subtitle")}</p>
    </div>

    <div class="section">
      <div class="sec-title">{t("ranking.scopeTitle")}</div>
      <label class="field">
        <select bind:value={scope}>
          <option value="japan_all">{t("ranking.scopeJapanAll")} ({catalog.totalJapanCount})</option>
          <option value="japan_region">{t("ranking.scopeJapanRegion")}</option>
          <option value="japan_pref">{t("ranking.scopeJapanPref")}</option>
          <option value="global">{t("ranking.scopeGlobal")} ({catalog.totalGlobalCount})</option>
          <option value="custom">{t("ranking.scopeCustom")} ({catalog.totalCustomCount})</option>
          <option value="all">{t("ranking.scopeAll")}</option>
        </select>
      </label>

      {#if scope === "japan_region"}
        <label class="field">
          <span>{t("ranking.regionLabel")}</span>
          <select bind:value={region}>
            <option value="all">{t("ranking.regions.all")}</option>
            {#each ["hokkaido", "tohoku", "kanto", "chubu", "kinki", "chugoku", "shikoku", "kyushu"] as reg}
              <option value={reg}>{regionLabel(reg)}</option>
            {/each}
          </select>
        </label>
      {:else if scope === "japan_pref"}
        <label class="field">
          <span>{t("ranking.prefectureLabel")}</span>
          <select bind:value={prefecture}>
            {#each catalog.prefectures ?? [] as pref, idx}
              <option value={pref}>
                {i18nState.lang === "en" ? (catalog.prefecturesEn?.[idx] ?? pref) : pref}
              </option>
            {/each}
          </select>
        </label>
      {:else if scope === "global"}
        <label class="field">
          <span>{t("ranking.regionLabel")}</span>
          <select bind:value={region}>
            <option value="all">{t("ranking.regions.all")}</option>
            {#each ["asia", "north_america", "europe", "oceania", "latin_america", "middle_east", "africa"] as reg}
              <option value={reg}>{regionLabel(reg)}</option>
            {/each}
          </select>
        </label>
      {/if}

      <button class="secondary-btn" type="button" onclick={() => (customModalOpen = !customModalOpen)}>
        {t("ranking.openCustomModal")}
      </button>

      {#if customModalOpen}
        <div class="custom-box">
          <div class="search-row">
            <input
              type="text"
              placeholder={t("location.searchPlaceholder")}
              bind:value={cityQuery}
              onkeydown={(e) => e.key === "Enter" && handleCitySearch()}
            />
            <button class="mini-btn" type="button" disabled={searchingCities} onclick={handleCitySearch}>
              {searchingCities ? "..." : t("location.searchBtn")}
            </button>
          </div>
          {#if citySearchResults.length > 0}
            <div class="city-results">
              {#each citySearchResults as r (r.id)}
                <button class="city-result-btn" type="button" onclick={() => addSearchedMunicipality(r)}>
                  <span>{r.name} {r.admin1 ? `(${r.admin1})` : ""} — {r.country}</span>
                  <span>+</span>
                </button>
              {/each}
            </div>
          {/if}

          <div class="row-2">
            <input type="text" placeholder={t("ranking.customNamePlaceholder")} bind:value={customName} />
            <input type="text" placeholder={t("ranking.customPrefPlaceholder")} bind:value={customPref} />
          </div>
          <div class="row-3">
            <select bind:value={customGroup}>
              <option value="japan">{t("ranking.customGroupJapan")}</option>
              <option value="global">{t("ranking.customGroupGlobal")}</option>
            </select>
            <input type="number" step="0.01" bind:value={customLat} placeholder="Lat" />
            <input type="number" step="0.01" bind:value={customLon} placeholder="Lon" />
          </div>
          <button class="chip-btn" type="button" onclick={addManualMunicipality}>{t("app.add")}</button>

          {#if (catalog.customLocations ?? []).length > 0}
            <div class="custom-list">
              <div class="sec-sub">{t("ranking.customListLabel")} ({(catalog.customLocations ?? []).length})</div>
              {#each catalog.customLocations ?? [] as cm (cm.id)}
                <div class="custom-item">
                  <span>{formatMuniName(cm)} ({formatMuniSub(cm)})</span>
                  <button class="mini-btn danger" type="button" onclick={() => removeCustomMunicipality(cm.id)}>✕</button>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <div class="section">
      <div class="sec-title">{t("ranking.presetsTitle")}</div>
      <div class="preset-grid">
        <button class="chip-btn" type="button" onclick={() => applyWeightPreset("balanced")}>
          {t("ranking.presetBalanced")}
        </button>
        <button class="chip-btn" type="button" onclick={() => applyWeightPreset("safety")}>
          {t("ranking.presetSafetyFirst")}
        </button>
        <button class="chip-btn" type="button" onclick={() => applyWeightPreset("comfort")}>
          {t("ranking.presetComfort")}
        </button>
        <button class="chip-btn" type="button" onclick={() => applyWeightPreset("snowfree")}>
          {t("ranking.presetSnowFree")}
        </button>
      </div>

      <div class="sec-sub">{t("ranking.weightsTitle")}</div>
      {#each factorList as f (f.id)}
        <div class="weight-row">
          <div class="weight-head">
            <span>{factorLabel(f.id)}</span>
            <strong>{weights[f.id] ?? f.defaultWeight}</strong>
          </div>
          <input
            type="range"
            min="0"
            max="100"
            step="5"
            value={weights[f.id] ?? f.defaultWeight}
            oninput={(e) => {
              weights = { ...weights, [f.id]: Number(e.currentTarget.value) };
            }}
          />
        </div>
      {/each}
    </div>

    <div class="section">
      <div class="sec-title">{t("ranking.conditionsTitle")}</div>
      <div class="row-2">
        <label class="field">
          <span>{t("ranking.rangeLabel")}</span>
          <select bind:value={rangeKey}>
            <option value="1y">1y</option>
            <option value="3y">3y</option>
            <option value="5y">5y</option>
            <option value="10y">10y</option>
            <option value="all">All</option>
          </select>
        </label>
        <label class="field">
          <span>{t("ranking.idealTemp")}</span>
          <input type="number" step="0.5" min="5" max="28" bind:value={idealTempC} />
        </label>
      </div>

      <label class="check-row">
        <input type="checkbox" bind:checked={penalizeExtremeHeat} />
        <span>{t("ranking.penalizeHeat")}</span>
      </label>
      <label class="check-row">
        <input type="checkbox" bind:checked={penalizeExtremeCold} />
        <span>{t("ranking.penalizeCold")}</span>
      </label>
      <label class="check-row">
        <input type="checkbox" bind:checked={penalizeHeavySnow} />
        <span>{t("ranking.penalizeSnow")}</span>
      </label>

      <div class="row-3">
        <label class="field">
          <span>{t("ranking.quakeRadius")}</span>
          <input type="number" min="10" max="300" step="5" bind:value={quakeRadiusKm} />
        </label>
        <label class="field">
          <span>{t("ranking.disasterRadius")}</span>
          <input type="number" min="5" max="200" step="5" bind:value={disasterRadiusKm} />
        </label>
        <label class="field">
          <span>{t("ranking.loreRadius")}</span>
          <input type="number" min="2" max="100" step="2" bind:value={loreRadiusKm} />
        </label>
      </div>

      <label class="check-row">
        <input type="checkbox" bind:checked={fetchMissingClimate} />
        <span>{t("ranking.fetchMissingClimate")}</span>
      </label>
      <p class="hint-text">{t("ranking.fetchMissingHint")}</p>
    </div>

    <div class="section">
      <div class="actions">
        {#if isRunning}
          <button class="pause-btn" type="button" onclick={pauseRanking}>{t("ranking.pause")}</button>
        {:else if isPaused}
          <button class="run-btn" type="button" onclick={resumeRanking}>{t("ranking.resume")}</button>
          <button class="secondary-btn" type="button" onclick={startRanking}>{t("ranking.start")}</button>
        {:else}
          <button class="run-btn" type="button" onclick={startRanking}>{t("ranking.start")}</button>
        {/if}
      </div>
    </div>
  </aside>

  <main class="rank-main">
    {#if errorMsg}
      <div class="error-banner">{errorMsg}</div>
    {/if}

    <section class="status-bar">
      <div class="status-top">
        <div class="status-left">
          <span class="status-badge status-{progress.status}">{statusBadgeLabel(progress.status)}</span>
          <span class="prog-text">
            {t("ranking.progressLabel")}: <strong>{progress.completed}</strong> / {progress.total} ({progressPct}%)
          </span>
          {#if progress.currentMunicipality}
            <span class="current-muni" title={progress.currentMunicipality}>
              {progress.currentMunicipality}
            </span>
          {/if}
        </div>
        <div class="status-metrics">
          <span>{t("ranking.computed")}: <strong>{progress.computed + progress.cacheHits}</strong></span>
          <span>{t("ranking.cacheHits")}: <strong>{progress.cacheHits}</strong></span>
          {#if progress.errors > 0}
            <span class="err-count">Errors: <strong>{progress.errors}</strong></span>
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
          placeholder={t("ranking.filterSearch")}
          bind:value={searchText}
        />
        <label class="inline-field">
          <span>{t("ranking.filterMinScore")}</span>
          <select bind:value={minScore}>
            <option value={0}>All (0+)</option>
            <option value={50}>50+</option>
            <option value={60}>60+</option>
            <option value={70}>70+</option>
            <option value={80}>80+</option>
            <option value={85}>85+</option>
          </select>
        </label>
        <label class="inline-field">
          <span>{t("ranking.sortBy")}</span>
          <select
            value={sortBy}
            onchange={(e) => handleToolbarSortChange(e.currentTarget.value as SortMode)}
          >
            <option value="total">{t("ranking.sortTotal")}</option>
            <option value="quake">{t("ranking.sortQuake")}</option>
            <option value="disaster">{t("ranking.sortDisaster")}</option>
            <option value="climate">{t("ranking.sortClimate")}</option>
            <option value="lore">{t("ranking.sortLore")}</option>
            <option value="volcano">{t("ranking.sortVolcano")}</option>
          </select>
        </label>
      </div>

      <div class="toolbar-right">
        <span class="count-pill">
          {filteredItems.length} / {items.length}
        </span>
        {#if filteredItems.length > 0}
          <button class="chip-btn" type="button" onclick={explainRankingViewWithAI}>
            ✨ {t("ranking.explainViewBtn")}
          </button>
        {/if}
        {#if confirmClear}
          <button class="danger-btn" type="button" onclick={clearRankingCache}>{t("ai.confirmDelete")}</button>
          <button class="chip-btn" type="button" onclick={() => (confirmClear = false)}>{t("app.cancel")}</button>
        {:else if items.length > 0}
          <button class="danger-outline-btn" type="button" onclick={() => (confirmClear = true)}>
            {t("ranking.clearAll")}
          </button>
        {/if}
      </div>
    </section>

    {#if items.length > 0}
      <div class="breakdown-legend">
        <span class="legend-title">{t("ranking.breakdownLegendLabel")}</span>
        {#each factorList as f (f.id)}
          <span class="legend-item">
            <span class="legend-tag">{f.id.slice(0, 2).toUpperCase()}</span>
            <span>{factorLabel(f.id)}</span>
          </span>
        {/each}
      </div>
    {/if}

    <div class="content-split" class:has-detail={selectedItem !== null}>
      <div class="table-wrap" role="region" aria-labelledby="ranking-table-caption">
        {#if filteredItems.length === 0}
          <div class="empty-state">
            <p>{t("ranking.empty")}</p>
          </div>
        {:else}
          <div class="sr-only" role="status">{sortStatusMsg}</div>
          <table class="rank-table">
            <caption id="ranking-table-caption" class="sr-only">{t("ranking.title")}</caption>
            <thead>
              <tr>
                <th
                  scope="col"
                  class="col-rank"
                  aria-sort={sortCol === "rank" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("rank", t("ranking.colRank"))}>
                    <span>{t("ranking.colRank")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th
                  scope="col"
                  aria-sort={sortCol === "total" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("total", t("ranking.colScore"))}>
                    <span>{t("ranking.colScore")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th
                  scope="col"
                  aria-sort={sortCol === "municipality" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("municipality", t("ranking.colMunicipality"))}>
                    <span>{t("ranking.colMunicipality")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th scope="col">{t("ranking.colBreakdown")}</th>
                <th
                  scope="col"
                  aria-sort={sortCol === "meanTemp" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("meanTemp", t("ranking.colClimate"))}>
                    <span>{t("ranking.colClimate")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th
                  scope="col"
                  aria-sort={sortCol === "quakeCount" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("quakeCount", t("ranking.colQuake"))}>
                    <span>{t("ranking.colQuake")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th
                  scope="col"
                  aria-sort={sortCol === "disasterCount" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("disasterCount", t("ranking.colDisaster"))}>
                    <span>{t("ranking.colDisaster")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th
                  scope="col"
                  aria-sort={sortCol === "volcanoDist" ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                >
                  <button type="button" class="th-sort-btn" onclick={() => toggleHeaderSort("volcanoDist", t("ranking.colVolcano"))}>
                    <span>{t("ranking.colVolcano")}</span>
                    <span class="sort-indicator" aria-hidden="true"></span>
                  </button>
                </th>
                <th scope="col" class="col-act">{t("ranking.colActions")}</th>
              </tr>
            </thead>
            <tbody>
              {#each filteredItems as item, idx (item.municipality.id)}
                {@const dispRank = item.rank > 0 ? item.rank : idx + 1}
                <tr
                  class:selected={selectedItem?.municipality.id === item.municipality.id}
                  onclick={() => (selectedItem = item)}
                >
                  <td class="col-rank">
                    <span class="rank-badge {rankClass(dispRank)}">#{dispRank}</span>
                  </td>
                  <td>
                    <span class="score-pill {scoreClass(item.totalScore)}">
                      {item.totalScore.toFixed(1)}
                    </span>
                  </td>
                  <td>
                    <div class="muni-name">{formatMuniName(item.municipality)}</div>
                    <div class="muni-sub">
                      {formatMuniSub(item.municipality)}
                      <span class="coord-text">({item.municipality.latitude.toFixed(2)}, {item.municipality.longitude.toFixed(2)})</span>
                    </div>
                  </td>
                  <td>
                    <div class="factor-bars">
                      {#each factorList as f (f.id)}
                        {@const fs = item.factorScores?.[f.id]}
                        {#if fs && fs.applicable}
                          <div class="mini-bar-item" title="{factorLabel(f.id)}: {fs.score.toFixed(1)}">
                            <span class="f-tag">{f.id.slice(0, 2).toUpperCase()}</span>
                            <div class="f-track">
                              <div class="f-fill {scoreClass(fs.score)}" style="width: {Math.min(100, Math.max(4, fs.score))}%"></div>
                            </div>
                            <span class="f-num">{fs.score.toFixed(0)}</span>
                          </div>
                        {/if}
                      {/each}
                    </div>
                  </td>
                  <td>
                    <div class="cell-primary">{item.meanTempC.toFixed(1)} °C</div>
                    <div class="cell-sub">Snow {item.annualSnowCm.toFixed(0)} cm</div>
                  </td>
                  <td>
                    <div class="cell-primary">{item.quakeCount}</div>
                    <div class="cell-sub">Max M{item.quakeMaxMag.toFixed(1)}</div>
                  </td>
                  <td>
                    <div class="cell-primary">{item.disasterCount} alerts</div>
                    {#if item.municipality.group === "japan"}
                      <div class="cell-sub">Lore {item.loreCount}</div>
                    {/if}
                  </td>
                  <td>
                    <div class="cell-primary">
                      {item.volcanoDistKm > 0 && item.volcanoDistKm < 9000 ? `${item.volcanoDistKm.toFixed(0)} km` : "—"}
                    </div>
                  </td>
                  <td class="col-act" onclick={(e) => e.stopPropagation()}>
                    <div class="row-actions">
                      <button
                        class="mini-btn"
                        type="button"
                        onclick={() => focusMapAt(item.municipality.latitude, item.municipality.longitude, 10)}
                      >
                        {t("ranking.focusMapBtn")}
                      </button>
                      <button
                        class="mini-btn"
                        type="button"
                        disabled={isWatched(item.municipality)}
                        onclick={() => addMunicipalityToWatched(item.municipality)}
                      >
                        {isWatched(item.municipality) ? t("ranking.addedObserve") : t("ranking.addObserveBtn")}
                      </button>
                      <button
                        class="chip-btn"
                        type="button"
                        onclick={() => explainMunicipalityWithAI(item)}
                      >
                        ✨
                      </button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </div>

      {#if selectedItem}
        <aside class="detail-pane">
          <div class="detail-header">
            <div>
              <div class="detail-rank">#{selectedItem.rank} · {formatMuniSub(selectedItem.municipality)}</div>
              <h3>{formatMuniName(selectedItem.municipality)}</h3>
            </div>
            <button class="mini-btn" type="button" onclick={() => (selectedItem = null)}>✕</button>
          </div>

          <div class="detail-score-box">
            <span class="score-pill large {scoreClass(selectedItem.totalScore)}">
              {selectedItem.totalScore.toFixed(1)} / 100
            </span>
            <button class="chip-btn" type="button" onclick={() => selectedItem && explainMunicipalityWithAI(selectedItem)}>
              ✨ {t("ranking.askAIBtn")}
            </button>
          </div>

          <div class="detail-factors">
            {#each factorList as f (f.id)}
              {@const fs = selectedItem.factorScores?.[f.id]}
              {#if fs}
                <div class="detail-factor-card">
                  <div class="df-head">
                    <span class="df-title">{factorLabel(f.id)}</span>
                    {#if fs.applicable}
                      <span class="score-pill {scoreClass(fs.score)}">{fs.score.toFixed(1)}</span>
                    {:else}
                      <span class="muted">N/A</span>
                    {/if}
                  </div>
                  {#if fs.applicable}
                    <div class="f-track wide">
                      <div class="f-fill {scoreClass(fs.score)}" style="width: {Math.min(100, Math.max(2, fs.score))}%"></div>
                    </div>
                    {#if fs.highlights && fs.highlights.length > 0}
                      <ul class="df-list">
                        {#each fs.highlights as h}
                          <li>{h}</li>
                        {/each}
                      </ul>
                    {:else}
                      <div class="df-summary">{fs.summary}</div>
                    {/if}
                  {/if}
                </div>
              {/if}
            {/each}
          </div>
        </aside>
      {/if}
    </div>
  </main>
</div>

<style>
  .rank-root {
    display: grid;
    grid-template-columns: 330px 1fr;
    gap: 12px;
    height: 100%;
    min-height: 0;
    color: #e2e8f0;
  }

  .rank-sidebar {
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
    margin-top: 4px;
  }

  .hint-text {
    margin: 0;
    font-size: 10.5px;
    color: #64748b;
    line-height: 1.35;
  }

  .row-2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }

  .row-3 {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 6px;
  }

  .preset-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 5px;
  }

  select,
  input[type="number"],
  input[type="text"],
  .search-input {
    background: #0f172a;
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 5px 7px;
    font-size: 12px;
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

  .weight-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .weight-head {
    display: flex;
    justify-content: space-between;
    font-size: 11.5px;
    color: #cbd5e1;
  }

  .weight-row input[type="range"] {
    width: 100%;
    accent-color: #38bdf8;
    cursor: pointer;
  }

  .custom-box {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px;
    background: rgba(2, 6, 23, 0.55);
    border: 1px solid rgba(56, 189, 248, 0.22);
    border-radius: 8px;
  }

  .search-row {
    display: flex;
    gap: 4px;
  }

  .city-results,
  .custom-list {
    display: flex;
    flex-direction: column;
    gap: 3px;
    max-height: 120px;
    overflow-y: auto;
  }

  .city-result-btn {
    display: flex;
    justify-content: space-between;
    background: rgba(30, 41, 59, 0.7);
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 5px;
    padding: 4px 7px;
    font-size: 11px;
    cursor: pointer;
    text-align: left;
  }
  .city-result-btn:hover {
    border-color: #38bdf8;
  }

  .custom-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 11px;
    color: #cbd5e1;
    padding: 2px 4px;
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

  .rank-main {
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

  .current-muni {
    color: #94a3b8;
    font-size: 11px;
    max-width: 300px;
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
    width: 240px;
  }

  .inline-field {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    color: #94a3b8;
  }
  .inline-field select {
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

  .content-split {
    display: grid;
    grid-template-columns: 1fr;
    gap: 10px;
    flex: 1 1 auto;
    min-height: 0;
  }
  .content-split.has-detail {
    grid-template-columns: 1fr 310px;
  }

  .table-wrap {
    min-height: 0;
    overflow: auto;
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 8px;
  }

  .rank-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .breakdown-legend {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
    padding: 6px 10px;
    background: rgba(30, 41, 59, 0.45);
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 7px;
    font-size: 11px;
    color: #cbd5e1;
  }

  .legend-title {
    color: #94a3b8;
    font-weight: 600;
  }

  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .legend-tag {
    display: inline-block;
    padding: 1px 5px;
    border-radius: 4px;
    background: rgba(56, 189, 248, 0.14);
    color: #7dd3fc;
    font-weight: 700;
    font-size: 10px;
  }

  .rank-table th {
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

  .rank-table th:has(> button) {
    padding: 0;
  }

  .th-sort-btn {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 4px;
    width: 100%;
    min-height: 30px;
    padding: 8px 10px;
    background: transparent;
    border: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .th-sort-btn:hover {
    color: #f8fafc;
    background: rgba(56, 189, 248, 0.08);
  }

  .th-sort-btn:focus-visible {
    outline: 2px solid #38bdf8;
    outline-offset: -2px;
  }

  .rank-table th[aria-sort] .th-sort-btn {
    color: #7dd3fc;
  }

  .sort-indicator {
    flex-shrink: 0;
    font-size: 10px;
    opacity: 0.35;
  }
  .sort-indicator::before {
    content: "⇅";
  }
  .rank-table th[aria-sort="ascending"] .sort-indicator {
    opacity: 1;
  }
  .rank-table th[aria-sort="ascending"] .sort-indicator::before {
    content: "▲";
  }
  .rank-table th[aria-sort="descending"] .sort-indicator {
    opacity: 1;
  }
  .rank-table th[aria-sort="descending"] .sort-indicator::before {
    content: "▼";
  }

  .rank-table td {
    padding: 7px 10px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.1);
    vertical-align: middle;
  }

  .rank-table tbody tr {
    cursor: pointer;
  }
  .rank-table tbody tr:hover,
  .rank-table tbody tr.selected {
    background: rgba(56, 189, 248, 0.1);
  }

  .col-rank {
    width: 54px;
  }
  .col-act {
    width: 165px;
  }

  .rank-badge {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 6px;
    font-weight: 700;
    font-size: 11px;
    background: rgba(148, 163, 184, 0.14);
    color: #cbd5e1;
  }
  .rank-1 {
    background: rgba(250, 204, 21, 0.24);
    color: #fde047;
    border: 1px solid rgba(250, 204, 21, 0.5);
  }
  .rank-2 {
    background: rgba(226, 232, 240, 0.22);
    color: #f8fafc;
    border: 1px solid rgba(226, 232, 240, 0.45);
  }
  .rank-3 {
    background: rgba(251, 146, 60, 0.22);
    color: #fdba74;
    border: 1px solid rgba(251, 146, 60, 0.45);
  }

  .score-pill {
    display: inline-block;
    padding: 3px 8px;
    border-radius: 6px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .score-pill.large {
    font-size: 15px;
    padding: 5px 12px;
  }
  .score-high {
    background: rgba(34, 197, 94, 0.22);
    color: #4ade80;
  }
  .score-mid {
    background: rgba(245, 158, 11, 0.22);
    color: #fbbf24;
  }
  .score-low {
    background: rgba(244, 63, 94, 0.2);
    color: #fda4af;
  }

  .muni-name {
    font-weight: 600;
    color: #f8fafc;
  }
  .muni-sub {
    font-size: 11px;
    color: #94a3b8;
  }
  .coord-text {
    color: #64748b;
    font-size: 10px;
  }

  .factor-bars {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 140px;
  }
  .mini-bar-item {
    display: grid;
    grid-template-columns: 22px 1fr 24px;
    align-items: center;
    gap: 4px;
    font-size: 10px;
  }
  .f-tag {
    color: #94a3b8;
    font-weight: 600;
  }
  .f-track {
    height: 5px;
    background: rgba(15, 23, 42, 0.9);
    border-radius: 999px;
    overflow: hidden;
  }
  .f-track.wide {
    height: 7px;
    margin: 4px 0;
  }
  .f-fill {
    height: 100%;
    border-radius: 999px;
  }
  .f-fill.score-high {
    background: #22c55e;
  }
  .f-fill.score-mid {
    background: #f59e0b;
  }
  .f-fill.score-low {
    background: #f43f5e;
  }
  .f-num {
    text-align: right;
    color: #cbd5e1;
    font-variant-numeric: tabular-nums;
  }

  .cell-primary {
    color: #e2e8f0;
    font-weight: 500;
  }
  .cell-sub {
    color: #94a3b8;
    font-size: 10.5px;
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
  .mini-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .mini-btn.danger:hover {
    background: rgba(239, 68, 68, 0.25);
    color: #fecaca;
  }

  .detail-pane {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    background: rgba(2, 6, 23, 0.65);
    border: 1px solid rgba(56, 189, 248, 0.25);
    border-radius: 8px;
    overflow-y: auto;
  }

  .detail-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
  }
  .detail-header h3 {
    margin: 2px 0 0;
    font-size: 15px;
    color: #f8fafc;
  }
  .detail-rank {
    font-size: 11px;
    color: #38bdf8;
    font-weight: 600;
  }

  .detail-score-box {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-bottom: 8px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.15);
  }

  .detail-factors {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .detail-factor-card {
    padding: 8px 10px;
    background: rgba(15, 23, 42, 0.85);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 8px;
  }

  .df-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    font-weight: 600;
  }

  .df-list {
    margin: 6px 0 0;
    padding-left: 16px;
    font-size: 11px;
    color: #cbd5e1;
    line-height: 1.45;
  }

  .df-summary {
    font-size: 11px;
    color: #94a3b8;
    margin-top: 4px;
  }

  .muted {
    color: #64748b;
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 220px;
    color: #94a3b8;
    font-size: 13px;
    padding: 20px;
    text-align: center;
  }
</style>
