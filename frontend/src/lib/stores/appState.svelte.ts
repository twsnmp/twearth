import { Events } from "@wailsio/runtime";
import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
import type { ViewExplainRequest } from "../../../bindings/github.com/twsnmp/twearth/internal/ai/models";
import type { AnalysisWindowOptions } from "../../../bindings/github.com/twsnmp/twearth/internal/app/models";
import type {
  CitySearchResult,
  HistorySyncStatus,
  KikikuruInfo,
  LidenSnapshot,
  MarineSnapshot,
  QuakeEvent,
  RadarTileInfo,
  SpaceWeatherSnapshot,
  SynopticData,
  VolcanoInfo,
  WeatherSnapshot,
  WindGridPoint,
} from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";
import type { Info as PlatformInfo } from "../../../bindings/github.com/twsnmp/twearth/internal/platform/models";
import type { Status as MCPStatus } from "../../../bindings/github.com/twsnmp/twearth/internal/mcp/models";
import type {
  Config,
  DisasterHistoryQuery,
  DisasterHistoryResult,
  DisasterLoreQuery,
  DisasterLoreRecord,
  DisasterRecord,
  EventRecord,
  RiverHistoryQuery,
  RiverHistoryResult,
  RiverTimeSeriesPoint,
} from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
import type { Status as UpdaterStatus } from "../../../bindings/github.com/twsnmp/twearth/internal/updater/models";
import { applyLanguageSetting, i18nState } from "../i18n/index.svelte";
import { evaluateQuakeSeverity } from "../alertSeverity";

export type MobileTab = "map" | "charts" | "marine" | "events";
export type WindowView = "main" | "charts" | "synoptic" | "marine" | "events" | "correlation" | "discovery" | "ranking";
export type WindowLayout = "grid" | "horizontal" | "vertical" | "cascade";

export interface WindowInitialParams {
  winId: string;
  mode?: string;
  locationId?: string;
  marineId?: string;
  rangeKey?: string;
  metric?: string;
  granularity?: string;
  showMovingAvg?: boolean;
  quakeRadiusKm?: number;
  cacheKey?: string;
}

export interface GeoCandidate {
  latitude: number;
  longitude: number;
  accuracy?: number;
  city?: string;
  source: "gps" | "ip";
  fetchedAt: string;
}

export const appState = $state({
  initialized: false,
  loading: false,
  error: "",
  windowSubtitle: "",
  activeTab: "map" as MobileTab,
  settingsOpen: false,
  settingsTab: "locations" as "locations" | "marine" | "general" | "ai" | "update",
  aiModal: {
    open: false,
    mode: "location" as "location" | "view" | "event" | "qa",
    locationId: "",
    viewReq: null as ViewExplainRequest | null,
    eventRecord: null as EventRecord | null,
    seq: 0,
  },
  focusCoord: null as { lat: number; lon: number; zoom?: number } | null,
  pickedCoord: null as { latitude: number; longitude: number } | null,
  selectedLocationId: "",
  selectedMarineId: "",
  geoCandidate: null as GeoCandidate | null,
  geoStatus: "idle" as "idle" | "fetching" | "ready" | "denied" | "unavailable",
  layers: {
    isobars: true,
    radar: true,
    kikikuru: "none" as "none" | "land" | "inund" | "flood",
    hazard: "none" as "none" | "flood" | "landslide" | "avalanche",
    jetstream: true,
    lightning: true,
    quakes: true,
    volcanoes: true,
    disasters: true,
    rivers: "off" as "off" | "nearby" | "all",
    lore: false,
    marine: true,
    currentValues: true,
  },
  platform: {
    os: "unknown",
    arch: "unknown",
    isMobile: false,
    canSelfUpdate: false,
    dataDir: "",
  } as PlatformInfo,
  config: null as Config | null,
  events: [] as EventRecord[],
  eventWindowHours: 168,
  eventsLoading: false,
  weather: null as WeatherSnapshot | null,
  weathers: [] as WeatherSnapshot[],
  marine: null as MarineSnapshot | null,
  marines: [] as MarineSnapshot[],
  windGrid: [] as WindGridPoint[],
  synoptic: null as SynopticData | null,
  liden: null as LidenSnapshot | null,
  radar: null as RadarTileInfo | null,
  kikikuru: null as KikikuruInfo | null,
  space: null as SpaceWeatherSnapshot | null,
  volcanoes: [] as VolcanoInfo[],
  quakes: [] as QuakeEvent[],
  disasters: [] as DisasterRecord[],
  rivers: [] as RiverTimeSeriesPoint[],
  loreCount: 0,
  lorePoints: [] as DisasterLoreRecord[],
  historySyncStatuses: [] as HistorySyncStatus[],
  updater: {
    supported: false,
    state: "unconfigured",
    currentVersion: "",
    latestVersion: "",
    releaseNotes: "",
    error: "",
  } as UpdaterStatus,
  mcp: {
    supported: false,
    running: false,
    port: 18088,
    endpoint: "",
    error: "",
  } as MCPStatus,
  downloadProgress: 0,
});

export function getWindowView(): WindowView {
  if (typeof window === "undefined") return "main";
  const v = new URLSearchParams(window.location.search).get("view");
  if (
    v === "charts" ||
    v === "synoptic" ||
    v === "marine" ||
    v === "events" ||
    v === "correlation" ||
    v === "discovery" ||
    v === "ranking"
  ) {
    return v;
  }
  return "main";
}

export function getWindowId(): string {
  if (typeof window === "undefined") return "";
  return new URLSearchParams(window.location.search).get("winId") ?? "";
}

export function getWindowInitialParams(): WindowInitialParams {
  if (typeof window === "undefined") return { winId: "" };
  const sp = new URLSearchParams(window.location.search);
  const maRaw = sp.get("ma");
  const radiusRaw = sp.get("radius");
  return {
    winId: sp.get("winId") ?? "",
    mode: sp.get("mode") ?? undefined,
    locationId: sp.get("loc") ?? undefined,
    marineId: sp.get("marineId") ?? undefined,
    rangeKey: sp.get("range") ?? undefined,
    metric: sp.get("metric") ?? undefined,
    granularity: sp.get("granularity") ?? undefined,
    showMovingAvg: maRaw === "true" ? true : maRaw === "false" ? false : undefined,
    quakeRadiusKm: radiusRaw !== null && radiusRaw !== "" && !Number.isNaN(Number(radiusRaw)) ? Number(radiusRaw) : undefined,
    cacheKey: sp.get("cacheKey") ?? undefined,
  };
}

export function hasUpdateBadge(): boolean {
  const s = appState.updater.state;
  return s === "available" || s === "ready" || s === "downloading" || s === "verifying" || s === "installing";
}

export function getActiveWeather(): WeatherSnapshot | null {
  if (appState.weathers.length > 0) {
    const found = appState.weathers.find((w) => w.locationId === appState.selectedLocationId);
    if (found) return found;
    return appState.weathers[0];
  }
  return appState.weather;
}

export function getActiveMarine(): MarineSnapshot | null {
  if (appState.marines.length > 0) {
    const found = appState.marines.find((m) => m.locationId === appState.selectedMarineId);
    if (found) return found;
    return appState.marines[0];
  }
  return appState.marine;
}

function syncSelectionsFromConfig(cfg: Config | null): void {
  if (!cfg) return;
  applyLanguageSetting(cfg.language);
  const configuredHours = cfg.eventWindowHours && cfg.eventWindowHours !== 0 ? cfg.eventWindowHours : 168;
  appState.eventWindowHours = configuredHours;
  const initial = getWindowInitialParams();
  const locs = cfg.locations ?? [];
  if (locs.length > 0 && !locs.some((l) => l.id === appState.selectedLocationId)) {
    const requested = initial.locationId ? locs.find((l) => l.id === initial.locationId) : undefined;
    const primary = requested ?? locs.find((l) => l.isPrimary) ?? locs[0];
    appState.selectedLocationId = primary.id;
  }
  const mars = cfg.marineLocations ?? [];
  if (mars.length > 0 && !mars.some((m) => m.id === appState.selectedMarineId)) {
    const requested = initial.marineId ? mars.find((m) => m.id === initial.marineId) : undefined;
    const primary = requested ?? mars.find((m) => m.isPrimary) ?? mars[0];
    appState.selectedMarineId = primary.id;
  }
}

export async function initAppState(): Promise<void> {
  if (appState.initialized) return;
  appState.loading = true;
  try {
    const [platform, config, updater, mcp, latest, syncStatuses] = await Promise.all([
      AppService.GetPlatformInfo(),
      AppService.GetConfig(),
      AppService.GetUpdaterStatus(),
      AppService.GetMCPStatus(),
      AppService.GetLatestState(),
      AppService.GetHistorySyncStatus(),
    ]);
    appState.platform = platform;
    appState.config = config;
    syncSelectionsFromConfig(config);
    appState.events = (await AppService.GetEventsSince(appState.eventWindowHours, 1000)) ?? [];
    appState.updater = updater;
    appState.mcp = mcp;
    appState.historySyncStatuses = syncStatuses ?? [];

    if (latest) {
      if (Array.isArray(latest.weathers) && latest.weathers.length > 0) {
        appState.weathers = latest.weathers;
        appState.weather = latest.weathers[0];
      }
      if (Array.isArray(latest.marines) && latest.marines.length > 0) {
        appState.marines = latest.marines;
        appState.marine = latest.marines[0];
      }
      if (Array.isArray(latest.windGrid)) appState.windGrid = latest.windGrid;
      if (latest.synoptic) appState.synoptic = latest.synoptic;
      if (latest.liden) appState.liden = latest.liden;
      if (latest.radar) appState.radar = latest.radar;
      if (latest.kikikuru) appState.kikikuru = latest.kikikuru;
      if (latest.space) appState.space = latest.space;
      if (Array.isArray(latest.volcanoes)) appState.volcanoes = latest.volcanoes;
      if (Array.isArray(latest.quakes)) appState.quakes = latest.quakes;
      if (Array.isArray(latest.disasters)) appState.disasters = latest.disasters;
      if (Array.isArray(latest.rivers)) appState.rivers = latest.rivers;
      if (typeof latest.loreCount === "number") appState.loreCount = latest.loreCount;
    }

    Events.On("twearth:history-sync-status", (ev: any) => {
      if (Array.isArray(ev?.data)) {
        appState.historySyncStatuses = ev.data;
      }
    });
    Events.On("twearth:config-updated", (ev: any) => {
      if (ev?.data) {
        appState.config = ev.data;
        syncSelectionsFromConfig(ev.data);
      }
    });
    Events.On("twearth:focus-map", (ev: any) => {
      if (ev?.data && getWindowView() === "main") {
        const { lat, lon, zoom } = ev.data;
        if (typeof lat === "number" && typeof lon === "number") {
          appState.focusCoord = {
            lat,
            lon,
            zoom: typeof zoom === "number" && zoom > 0 ? zoom : 7,
          };
          if (appState.platform.isMobile) {
            appState.activeTab = "map";
          }
        }
      }
    });
    Events.On("twearth:weather-updated", (ev: any) => {
      if (ev?.data) appState.weather = ev.data;
    });
    Events.On("twearth:weathers-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) {
        appState.weathers = ev.data;
        if (ev.data.length > 0 && !appState.weather) {
          appState.weather = ev.data[0];
        }
      }
    });
    Events.On("twearth:windgrid-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.windGrid = ev.data;
    });
    Events.On("twearth:synoptic-updated", (ev: any) => {
      if (ev?.data) appState.synoptic = ev.data;
    });
    Events.On("twearth:marine-updated", (ev: any) => {
      if (ev?.data) appState.marine = ev.data;
    });
    Events.On("twearth:marines-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) {
        appState.marines = ev.data;
        if (ev.data.length > 0 && !appState.marine) {
          appState.marine = ev.data[0];
        }
      }
    });
    Events.On("twearth:liden-updated", (ev: any) => {
      if (ev?.data) appState.liden = ev.data;
    });
    Events.On("twearth:radar-updated", (ev: any) => {
      if (ev?.data) appState.radar = ev.data;
    });
    Events.On("twearth:kikikuru-updated", (ev: any) => {
      if (ev?.data) appState.kikikuru = ev.data;
    });
    Events.On("twearth:disasters-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.disasters = ev.data;
    });
    Events.On("twearth:rivers-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.rivers = ev.data;
    });
    Events.On("twearth:lore-updated", (ev: any) => {
      if (typeof ev?.data === "number") appState.loreCount = ev.data;
    });
    Events.On("twearth:space-updated", (ev: any) => {
      if (ev?.data) appState.space = ev.data;
    });
    Events.On("twearth:volcano-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.volcanoes = ev.data;
    });
    Events.On("twearth:quake-history", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.quakes = mergeQuakes(appState.quakes, ev.data);
    });
    Events.On("twearth:quake-usgs", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.quakes = mergeQuakes(appState.quakes, ev.data);
    });
    Events.On("twearth:alert-event", (ev: any) => {
      if (ev?.data) {
        const rec: EventRecord = ev.data;
        appState.events = [rec, ...appState.events.filter((e) => e.id !== rec.id)].slice(0, 1000);
      }
    });
    Events.On("twearth:quake-event", (ev: any) => {
      if (ev?.data) {
        const q: QuakeEvent = ev.data;
        appState.quakes = mergeQuakes(appState.quakes, [q]);
        if (q.code === 552 && q.tsunami === "Cancelled") {
          return;
        }
        let titleKey = "event.quake.title";
        if (q.code === 552) {
          if (q.tsunami === "MajorWarning") titleKey = "event.tsunamiMajor.title";
          else if (q.tsunami === "Warning") titleKey = "event.tsunamiWarning.title";
          else if (q.tsunami === "Watch") titleKey = "event.tsunamiWatch.title";
          else titleKey = "event.tsunami.title";
        } else if (q.code === 556) {
          titleKey = "event.eew.title";
        }
        const sevRes = evaluateQuakeSeverity(q, appState.config);
        appState.events = [
          {
            id: q.id,
            type: "TWEARTH-QUAKE",
            severity: sevRes.severity,
            baseSeverity: sevRes.baseSeverity,
            title: q.title,
            detail: q.hypocenter,
            titleKey,
            detailKey: "event.quake.detail",
            params: {
              hypocenter: q.hypocenter,
              magnitude: q.magnitude.toFixed(1),
              maxScale: q.maxScale,
              depth: q.depthKm.toFixed(0),
              source: q.source,
              code: String(q.code ?? 551),
              tsunami: q.tsunami ?? "",
            },
            latitude: q.latitude,
            longitude: q.longitude,
            timestamp: q.occurredAt,
          },
          ...appState.events.filter((e) => e.id !== q.id),
        ].slice(0, 1000);
      }
    });

    if (platform.canSelfUpdate) {
      Events.On("wails:updater:check-started", () => {
        appState.updater = { ...appState.updater, state: "checking", error: "" };
      });
      Events.On("wails:updater:no-update", () => {
        appState.updater = { ...appState.updater, state: "up-to-date" };
      });
      Events.On("wails:updater:update-available", (ev: any) => {
        appState.updater = {
          ...appState.updater,
          state: "available",
          latestVersion: ev?.data?.version ?? appState.updater.latestVersion,
          releaseNotes: ev?.data?.notes ?? appState.updater.releaseNotes,
        };
      });
      Events.On("wails:updater:download-progress", (ev: any) => {
        const p = ev?.data;
        if (p?.total > 0) {
          appState.downloadProgress = Math.round((p.written / p.total) * 100);
        }
        appState.updater = { ...appState.updater, state: "downloading" };
      });
      Events.On("wails:updater:verifying", () => {
        appState.updater = { ...appState.updater, state: "verifying" };
      });
      Events.On("wails:updater:installing", () => {
        appState.updater = { ...appState.updater, state: "installing" };
      });
      Events.On("wails:updater:update-ready", () => {
        appState.updater = { ...appState.updater, state: "ready" };
      });
      Events.On("wails:updater:error", (ev: any) => {
        appState.updater = {
          ...appState.updater,
          state: "error",
          error: ev?.data?.message ?? "Update failed",
        };
      });
    }

    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") {
          void AppService.NotifyAppForeground();
        }
      });
    }

    // Attempt silent current-location detection so it is ready as a registration candidate.
    detectCurrentLocation();

    appState.initialized = true;
  } catch (err) {
    appState.error = err instanceof Error ? err.message : String(err);
  } finally {
    appState.loading = false;
  }
}

async function detectViaIPFallback(deniedReason: "denied" | "unavailable"): Promise<void> {
  try {
    const res = await AppService.DetectCurrentLocation();
    if (res && (res.latitude !== 0 || res.longitude !== 0)) {
      appState.geoCandidate = {
        latitude: Number(res.latitude.toFixed(4)),
        longitude: Number(res.longitude.toFixed(4)),
        city: res.city || res.region || "",
        source: "ip",
        fetchedAt: new Date().toISOString(),
      };
      appState.geoStatus = "ready";
      return;
    }
  } catch {
    // Ignore IP fallback failure and report original reason
  }
  appState.geoStatus = deniedReason;
}

export function detectCurrentLocation(): void {
  if (typeof navigator === "undefined" || !("geolocation" in navigator)) {
    appState.geoStatus = "fetching";
    void detectViaIPFallback("unavailable");
    return;
  }
  appState.geoStatus = "fetching";
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      appState.geoCandidate = {
        latitude: Number(pos.coords.latitude.toFixed(4)),
        longitude: Number(pos.coords.longitude.toFixed(4)),
        accuracy: Math.round(pos.coords.accuracy),
        source: "gps",
        fetchedAt: new Date().toISOString(),
      };
      appState.geoStatus = "ready";
    },
    (err) => {
      const reason = err.code === err.PERMISSION_DENIED ? "denied" : "unavailable";
      void detectViaIPFallback(reason);
    },
    { enableHighAccuracy: false, timeout: 6000, maximumAge: 300000 }
  );
}

function mergeQuakes(existing: QuakeEvent[], incoming: QuakeEvent[]): QuakeEvent[] {
  const map = new Map<string, QuakeEvent>();
  for (const q of existing) map.set(q.id, q);
  for (const q of incoming) map.set(q.id, q);
  return Array.from(map.values())
    .sort((a, b) => new Date(b.occurredAt).getTime() - new Date(a.occurredAt).getTime())
    .slice(0, 80);
}

export function focusMapAt(lat: number, lon: number, zoom = 7): void {
  if (getWindowView() !== "main") {
    void AppService.FocusMap(lat, lon, zoom);
    return;
  }
  appState.focusCoord = { lat, lon, zoom };
  if (appState.platform.isMobile) {
    appState.activeTab = "map";
  }
}

export function selectLocation(id: string, focusMap = true): void {
  appState.selectedLocationId = id;
  if (focusMap) {
    const loc = appState.config?.locations?.find((l) => l.id === id);
    if (loc) {
      appState.focusCoord = { lat: loc.latitude, lon: loc.longitude, zoom: 7 };
    }
  }
}

export function selectMarineLocation(id: string, focusMap = true): void {
  appState.selectedMarineId = id;
  if (focusMap) {
    const loc = appState.config?.marineLocations?.find((l) => l.id === id);
    if (loc) {
      appState.focusCoord = { lat: loc.latitude, lon: loc.longitude, zoom: 7 };
    }
  }
}

export async function setEventWindowHours(hours: number): Promise<void> {
  appState.eventWindowHours = hours;
  appState.eventsLoading = true;
  try {
    appState.events = (await AppService.GetEventsSince(hours, 1000)) ?? [];
  } finally {
    appState.eventsLoading = false;
  }
}

export async function refreshMCPStatus(): Promise<void> {
  try {
    appState.mcp = await AppService.GetMCPStatus();
  } catch {
    // Ignore transient errors
  }
}

export async function saveAppConfig(next: Config): Promise<void> {
  const prevHours = appState.eventWindowHours;
  await AppService.SaveConfig(next);
  appState.config = next;
  syncSelectionsFromConfig(next);
  await refreshMCPStatus();
  if (appState.eventWindowHours !== prevHours) {
    await setEventWindowHours(appState.eventWindowHours);
  }
}

export async function toggleLanguage(): Promise<void> {
  const nextLang = i18nState.lang === "ja" ? "en" : "ja";
  applyLanguageSetting(nextLang);
  if (appState.config) {
    const updated: Config = { ...appState.config, language: nextLang };
    await saveAppConfig(updated);
  }
}

export async function searchCities(query: string): Promise<CitySearchResult[]> {
  return (await AppService.SearchCities(query, i18nState.lang)) ?? [];
}

export async function openAnalysisWindow(view: Exclude<WindowView, "main">): Promise<void> {
  await AppService.OpenAnalysisWindow(view);
}

export async function openAnalysisWindowWithOptions(opts: AnalysisWindowOptions): Promise<void> {
  await AppService.OpenAnalysisWindowWithOptions(opts);
}

export async function setAnalysisWindowTitle(winId: string, title: string): Promise<void> {
  if (!winId || !title) return;
  await AppService.SetAnalysisWindowTitle(winId, title);
}

export async function arrangeAnalysisWindows(layout: WindowLayout): Promise<void> {
  await AppService.ArrangeAnalysisWindows(layout);
}

export async function closeAllAnalysisWindows(): Promise<void> {
  await AppService.CloseAllAnalysisWindows();
}

export async function refreshNow(): Promise<void> {
  await AppService.RefreshNow();
}

export async function checkForUpdate(): Promise<void> {
  if (!appState.platform.canSelfUpdate) return;
  appState.updater = await AppService.CheckForUpdate();
}

export async function installUpdate(): Promise<void> {
  if (!appState.platform.canSelfUpdate) return;
  await AppService.DownloadAndInstallUpdate();
}

export async function restartApp(): Promise<void> {
  if (!appState.platform.canSelfUpdate) return;
  await AppService.RestartAfterUpdate();
}

export async function queryDisasterHistory(req: DisasterHistoryQuery): Promise<DisasterHistoryResult> {
  return AppService.QueryDisasterHistory(req);
}

export async function queryRiverHistory(req: RiverHistoryQuery): Promise<RiverHistoryResult> {
  return AppService.QueryRiverHistory(req);
}

export async function queryDisasterLore(req: DisasterLoreQuery): Promise<DisasterLoreRecord[]> {
  return (await AppService.QueryDisasterLore(req)) ?? [];
}

export async function syncDisasterLore(): Promise<number> {
  const cnt = await AppService.SyncDisasterLore();
  appState.loreCount = cnt;
  return cnt;
}

export function openLocationAI(locationId?: string): void {
  appState.aiModal = {
    open: true,
    mode: "location",
    locationId: locationId || appState.selectedLocationId || appState.config?.locations?.[0]?.id || "",
    viewReq: null,
    eventRecord: null,
    seq: appState.aiModal.seq + 1,
  };
}

export function openViewAI(req: ViewExplainRequest): void {
  appState.aiModal = {
    open: true,
    mode: "view",
    locationId: req.locationId || appState.selectedLocationId || "",
    viewReq: req,
    eventRecord: null,
    seq: appState.aiModal.seq + 1,
  };
}

export function openEventAI(ev: EventRecord): void {
  appState.aiModal = {
    open: true,
    mode: "event",
    locationId: appState.selectedLocationId || "",
    viewReq: null,
    eventRecord: ev,
    seq: appState.aiModal.seq + 1,
  };
}

export function openQAAI(): void {
  appState.aiModal = {
    open: true,
    mode: "qa",
    locationId: appState.selectedLocationId || "",
    viewReq: null,
    eventRecord: null,
    seq: appState.aiModal.seq + 1,
  };
}

export function closeAIModal(): void {
  appState.aiModal.open = false;
}
