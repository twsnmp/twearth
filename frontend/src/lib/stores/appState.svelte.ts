import { Events } from "@wailsio/runtime";
import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
import type {
  LidenSnapshot,
  MarineSnapshot,
  QuakeEvent,
  SpaceWeatherSnapshot,
  VolcanoInfo,
  WeatherSnapshot,
  WindGridPoint,
} from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";
import type { Info as PlatformInfo } from "../../../bindings/github.com/twsnmp/twearth/internal/platform/models";
import type { Config, EventRecord } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";
import type { Status as UpdaterStatus } from "../../../bindings/github.com/twsnmp/twearth/internal/updater/models";

export type MobileTab = "map" | "charts" | "events" | "settings";

export const appState = $state({
  initialized: false,
  loading: false,
  error: "",
  activeTab: "map" as MobileTab,
  settingsOpen: false,
  focusCoord: null as { lat: number; lon: number; zoom?: number } | null,
  platform: {
    os: "unknown",
    arch: "unknown",
    isMobile: false,
    canSelfUpdate: false,
    dataDir: "",
  } as PlatformInfo,
  config: null as Config | null,
  events: [] as EventRecord[],
  weather: null as WeatherSnapshot | null,
  marine: null as MarineSnapshot | null,
  windGrid: [] as WindGridPoint[],
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

export async function initAppState(): Promise<void> {
  if (appState.initialized) return;
  appState.loading = true;
  try {
    const [platform, config, events, updater] = await Promise.all([
      AppService.GetPlatformInfo(),
      AppService.GetConfig(),
      AppService.GetRecentEvents(50),
      AppService.GetUpdaterStatus(),
    ]);
    appState.platform = platform;
    appState.config = config;
    appState.events = events ?? [];
    appState.updater = updater;

    Events.On("twearth:config-updated", (ev: any) => {
      if (ev?.data) appState.config = ev.data;
    });
    Events.On("twearth:weather-updated", (ev: any) => {
      if (ev?.data) appState.weather = ev.data;
    });
    Events.On("twearth:windgrid-updated", (ev: any) => {
      if (Array.isArray(ev?.data)) appState.windGrid = ev.data;
    });
    Events.On("twearth:marine-updated", (ev: any) => {
      if (ev?.data) appState.marine = ev.data;
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
    Events.On("twearth:quake-event", (ev: any) => {
      if (ev?.data) {
        const q: QuakeEvent = ev.data;
        appState.quakes = mergeQuakes(appState.quakes, [q]);
        appState.events = [
          {
            id: q.id,
            type: "TWEARTH-QUAKE",
            severity: q.magnitude >= 5.0 ? "warning" : "info",
            title: q.title,
            detail: q.hypocenter,
            latitude: q.latitude,
            longitude: q.longitude,
            timestamp: q.occurredAt,
          },
          ...appState.events,
        ].slice(0, 100);
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

    appState.initialized = true;
  } catch (err) {
    appState.error = err instanceof Error ? err.message : String(err);
  } finally {
    appState.loading = false;
  }
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

export async function saveAppConfig(next: Config): Promise<void> {
  await AppService.SaveConfig(next);
  appState.config = next;
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
