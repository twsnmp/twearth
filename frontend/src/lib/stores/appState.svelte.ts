import { Events } from "@wailsio/runtime";
import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
import type {
  CitySearchResult,
  LidenSnapshot,
  MarineSnapshot,
  QuakeEvent,
  SpaceWeatherSnapshot,
  SynopticData,
  VolcanoInfo,
  WeatherSnapshot,
  WindGridPoint,
} from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";
import type { Info as PlatformInfo } from "../../../bindings/github.com/twsnmp/twearth/internal/platform/models";
import type { Config, EventRecord } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
import type { Status as UpdaterStatus } from "../../../bindings/github.com/twsnmp/twearth/internal/updater/models";
import { applyLanguageSetting, i18nState } from "../i18n/index.svelte";

export type MobileTab = "map" | "charts" | "marine" | "events";
export type WindowView = "main" | "charts" | "synoptic" | "marine" | "events";

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
  activeTab: "map" as MobileTab,
  settingsOpen: false,
  settingsTab: "locations" as "locations" | "marine" | "general" | "update",
  focusCoord: null as { lat: number; lon: number; zoom?: number } | null,
  pickedCoord: null as { latitude: number; longitude: number } | null,
  selectedLocationId: "",
  selectedMarineId: "",
  geoCandidate: null as GeoCandidate | null,
  geoStatus: "idle" as "idle" | "fetching" | "ready" | "denied" | "unavailable",
  layers: {
    isobars: true,
    jetstream: true,
    lightning: true,
    quakes: true,
    volcanoes: true,
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
  space: null as SpaceWeatherSnapshot | null,
  volcanoes: [] as VolcanoInfo[],
  quakes: [] as QuakeEvent[],
  updater: {
    supported: false,
    state: "unconfigured",
    currentVersion: "",
    latestVersion: "",
    releaseNotes: "",
    error: "",
  } as UpdaterStatus,
  downloadProgress: 0,
});

export function getWindowView(): WindowView {
  if (typeof window === "undefined") return "main";
  const v = new URLSearchParams(window.location.search).get("view");
  if (v === "charts" || v === "synoptic" || v === "marine" || v === "events") {
    return v;
  }
  return "main";
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
  const locs = cfg.locations ?? [];
  if (locs.length > 0 && !locs.some((l) => l.id === appState.selectedLocationId)) {
    const primary = locs.find((l) => l.isPrimary) ?? locs[0];
    appState.selectedLocationId = primary.id;
  }
  const mars = cfg.marineLocations ?? [];
  if (mars.length > 0 && !mars.some((m) => m.id === appState.selectedMarineId)) {
    const primary = mars.find((m) => m.isPrimary) ?? mars[0];
    appState.selectedMarineId = primary.id;
  }
}

export async function initAppState(): Promise<void> {
  if (appState.initialized) return;
  appState.loading = true;
  try {
    const [platform, config, updater, latest] = await Promise.all([
      AppService.GetPlatformInfo(),
      AppService.GetConfig(),
      AppService.GetUpdaterStatus(),
      AppService.GetLatestState(),
    ]);
    appState.platform = platform;
    appState.config = config;
    syncSelectionsFromConfig(config);
    appState.events = (await AppService.GetEventsSince(appState.eventWindowHours, 1000)) ?? [];
    appState.updater = updater;

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
      if (latest.space) appState.space = latest.space;
      if (Array.isArray(latest.volcanoes)) appState.volcanoes = latest.volcanoes;
      if (Array.isArray(latest.quakes)) appState.quakes = latest.quakes;
    }

    Events.On("twearth:config-updated", (ev: any) => {
      if (ev?.data) {
        appState.config = ev.data;
        syncSelectionsFromConfig(ev.data);
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
        const isCritical =
          q.code === 552 ||
          q.code === 556 ||
          q.magnitude >= 6.0 ||
          ["5-", "5+", "6-", "6+", "7", "5弱", "5強", "6弱", "6強"].includes((q.maxScale || "").trim());
        const isWarning =
          !isCritical &&
          (q.magnitude >= 4.5 || ["3", "4"].includes((q.maxScale || "").trim()));
        appState.events = [
          {
            id: q.id,
            type: "TWEARTH-QUAKE",
            severity: isCritical ? "critical" : isWarning ? "warning" : "info",
            title: q.title,
            detail: q.hypocenter,
            titleKey: q.code === 552 ? "event.tsunami.title" : q.code === 556 ? "event.eew.title" : "event.quake.title",
            detailKey: "event.quake.detail",
            params: {
              hypocenter: q.hypocenter,
              magnitude: q.magnitude.toFixed(1),
              maxScale: q.maxScale,
              depth: q.depthKm.toFixed(0),
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

export async function saveAppConfig(next: Config): Promise<void> {
  const prevHours = appState.eventWindowHours;
  await AppService.SaveConfig(next);
  appState.config = next;
  syncSelectionsFromConfig(next);
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
