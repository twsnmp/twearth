import type { QuakeEvent } from "../../bindings/github.com/twsnmp/twearth/internal/collector/models";
import type {
  Config,
  DisasterRecord,
  EventRecord,
} from "../../bindings/github.com/twsnmp/twearth/internal/store/models";

export type Severity = "critical" | "warning" | "info";
export type AlertCategory =
  | "quake"
  | "disaster"
  | "volcano"
  | "lightning"
  | "pressure"
  | "precip"
  | "space"
  | "other";

export interface AlertSeverityMeta {
  source?: string;
  category?: AlertCategory | string;
  code?: number;
  magnitude?: number;
  maxScale?: string;
  tsunami?: string;
  latitude?: number | null;
  longitude?: number | null;
}

export interface AdjustedSeverityResult {
  severity: Severity;
  baseSeverity: Severity;
  attenuated: boolean;
  distanceKm: number | null;
  nearestLocationName: string;
  nearestLocationNameEn: string;
}

export function scaleRank(scale?: string): number {
  switch ((scale ?? "").trim()) {
    case "1":
      return 10;
    case "2":
      return 20;
    case "3":
      return 30;
    case "4":
      return 40;
    case "5-":
    case "5弱":
      return 45;
    case "5+":
    case "5強":
      return 50;
    case "6-":
    case "6弱":
      return 55;
    case "6+":
    case "6強":
      return 60;
    case "7":
      return 70;
    default:
      return 0;
  }
}

export function normalizeSeverity(s?: string): Severity {
  switch ((s ?? "").trim().toLowerCase()) {
    case "critical":
      return "critical";
    case "warning":
      return "warning";
    default:
      return "info";
  }
}

function severityToRank(s: Severity): number {
  if (s === "critical") return 3;
  if (s === "warning") return 2;
  return 1;
}

function rankToSeverity(r: number): Severity {
  if (r >= 3) return "critical";
  if (r === 2) return "warning";
  return "info";
}

export function haversineKm(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const rad = Math.PI / 180;
  const dLat = (lat2 - lat1) * rad;
  const dLon = (lon2 - lon1) * rad;
  const a =
    Math.sin(dLat / 2) * Math.sin(dLat / 2) +
    Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(dLon / 2) * Math.sin(dLon / 2);
  return 6371 * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

export function getNearRadiusKm(config?: Config | null): number {
  const v = config?.alertNearRadiusKm;
  return typeof v === "number" && v > 0 ? v : 100;
}

export function getMediumRadiusKm(config?: Config | null): number {
  const near = getNearRadiusKm(config);
  const v = config?.alertMediumRadiusKm;
  const med = typeof v === "number" && v > 0 ? v : 300;
  return med < near ? near : med;
}

export function distanceToReferenceLocations(
  lat?: number | null,
  lon?: number | null,
  config?: Config | null
): { distanceKm: number | null; locationName: string; locationNameEn: string } {
  if (
    lat === undefined ||
    lon === undefined ||
    lat === null ||
    lon === null ||
    !Number.isFinite(lat) ||
    !Number.isFinite(lon) ||
    (lat === 0 && lon === 0)
  ) {
    return { distanceKm: null, locationName: "", locationNameEn: "" };
  }

  const locs = config?.locations ?? [];
  if (locs.length === 0) {
    return { distanceKm: null, locationName: "", locationNameEn: "" };
  }

  const primary = locs.find((l) => l.isPrimary) ?? locs[0];
  const refs = config?.alertDistanceMode === "primary" ? [primary] : locs;

  let best = Number.POSITIVE_INFINITY;
  let bestName = "";
  let bestNameEn = "";
  for (const loc of refs) {
    if (loc.latitude === 0 && loc.longitude === 0) continue;
    const d = haversineKm(loc.latitude, loc.longitude, lat, lon);
    if (d < best) {
      best = d;
      bestName = loc.name || loc.nameEn || "";
      bestNameEn = loc.nameEn || loc.name || "";
    }
  }

  if (!Number.isFinite(best)) {
    return { distanceKm: null, locationName: "", locationNameEn: "" };
  }
  return { distanceKm: best, locationName: bestName, locationNameEn: bestNameEn };
}

export function classifyQuakeBaseSeverity(
  q: { code?: number; tsunami?: string; maxScale?: string; magnitude?: number },
  minIntensity?: string
): Severity {
  if (q.code === 552) {
    const tsu = (q.tsunami ?? "").toLowerCase();
    if (tsu === "cancelled") return "info";
    if (tsu === "watch") return "warning";
    return "critical";
  }
  if (q.code === 556) return "critical";

  const rank = scaleRank(q.maxScale);
  const minRank = scaleRank(minIntensity) || 30;
  const mag = q.magnitude ?? 0;

  if (rank >= 45 || mag >= 6.0) return "critical";
  if ((rank > 0 && rank >= minRank) || (rank === 0 && mag >= 5.0)) return "warning";
  return "info";
}

export function adjustSeverityByDistance(
  baseInput: Severity | string,
  meta: AlertSeverityMeta,
  config?: Config | null
): AdjustedSeverityResult {
  const baseSeverity = normalizeSeverity(baseInput);
  const baseRank = severityToRank(baseSeverity);

  if (meta.category === "space" || meta.source === "space") {
    return {
      severity: baseSeverity,
      baseSeverity,
      attenuated: false,
      distanceKm: null,
      nearestLocationName: "",
      nearestLocationNameEn: "",
    };
  }

  const { distanceKm, locationName, locationNameEn } = distanceToReferenceLocations(
    meta.latitude,
    meta.longitude,
    config
  );
  if (distanceKm === null) {
    if (meta.code === 552) {
      const tsu = (meta.tsunami ?? "").toLowerCase();
      if (tsu === "cancelled") {
        return {
          severity: "info",
          baseSeverity,
          attenuated: baseRank > 1,
          distanceKm: null,
          nearestLocationName: "",
          nearestLocationNameEn: "",
        };
      }
      if (tsu === "watch" && baseRank > 2) {
        return {
          severity: "warning",
          baseSeverity,
          attenuated: true,
          distanceKm: null,
          nearestLocationName: "",
          nearestLocationNameEn: "",
        };
      }
    }
    return {
      severity: baseSeverity,
      baseSeverity,
      attenuated: false,
      distanceKm: null,
      nearestLocationName: "",
      nearestLocationNameEn: "",
    };
  }

  const nearKm = getNearRadiusKm(config);
  const mediumKm = getMediumRadiusKm(config);

  let rank = baseRank;
  if (distanceKm > mediumKm) {
    rank = 1;
  } else if (distanceKm > nearKm && rank > 1) {
    rank -= 1;
  }

  // Major domestic/regional earthquake and active tsunami warning exception (minimum "warning").
  if (baseRank >= 2 && rank < 2) {
    const tsu = (meta.tsunami ?? "").toLowerCase();
    const isActiveTsunamiWarning =
      meta.code === 552 && tsu !== "cancelled" && tsu !== "watch" && tsu !== "none";
    if (isActiveTsunamiWarning && (meta.source === "p2pquake" || distanceKm <= 1500)) {
      rank = 2;
    }

    const mag = meta.magnitude ?? 0;
    const isMajorDomesticQuake =
      scaleRank(meta.maxScale) >= 55 ||
      (mag >= 7.0 && (meta.source === "p2pquake" || distanceKm <= 1500));
    if (isMajorDomesticQuake) {
      rank = 2;
    }
  }

  const severity = rankToSeverity(rank);
  return {
    severity,
    baseSeverity,
    attenuated: rank < baseRank,
    distanceKm,
    nearestLocationName: locationName,
    nearestLocationNameEn: locationNameEn,
  };
}

export function evaluateQuakeSeverity(
  q: QuakeEvent,
  config?: Config | null
): AdjustedSeverityResult {
  const base = classifyQuakeBaseSeverity(q, config?.quakeMinIntensity);
  return adjustSeverityByDistance(
    base,
    {
      source: q.source,
      category: "quake",
      code: q.code,
      magnitude: q.magnitude,
      maxScale: q.maxScale,
      tsunami: q.tsunami,
      latitude: q.latitude,
      longitude: q.longitude,
    },
    config
  );
}

export function evaluateDisasterSeverity(
  d: DisasterRecord,
  config?: Config | null
): AdjustedSeverityResult {
  return adjustSeverityByDistance(
    normalizeSeverity(d.severity),
    {
      source: d.source,
      category: "disaster",
      latitude: d.latitude,
      longitude: d.longitude,
    },
    config
  );
}

export function evaluateEventSeverity(
  ev: EventRecord,
  cat: AlertCategory,
  config?: Config | null
): AdjustedSeverityResult {
  const params = ev.params ?? {};

  if (cat === "quake") {
    const parsedCode = params.code ? parseInt(params.code, 10) : 0;
    let code = Number.isNaN(parsedCode) ? 0 : parsedCode;
    if (code === 0) {
      if (
        ev.titleKey === "event.tsunami.title" ||
        ev.titleKey === "event.tsunamiMajor.title" ||
        ev.titleKey === "event.tsunamiWarning.title" ||
        ev.titleKey === "event.tsunamiWatch.title"
      ) {
        code = 552;
      } else if (ev.titleKey === "event.eew.title") {
        code = 556;
      } else {
        code = 551;
      }
    }
    const mag = parseFloat(params.magnitude ?? "");
    const magnitude = Number.isNaN(mag) ? 0 : mag;
    const maxScale = params.maxScale ?? "";
    const source =
      params.source ||
      (maxScale && maxScale !== "-" ? "p2pquake" : ev.id.startsWith("usgs-") ? "usgs" : "p2pquake");
    const tsunami = params.tsunami ?? "";

    const base = classifyQuakeBaseSeverity(
      { code, tsunami, maxScale, magnitude },
      config?.quakeMinIntensity
    );
    return adjustSeverityByDistance(
      base,
      {
        source,
        category: "quake",
        code,
        magnitude,
        maxScale,
        tsunami,
        latitude: ev.latitude,
        longitude: ev.longitude,
      },
      config
    );
  }

  let base: Severity = ev.baseSeverity
    ? normalizeSeverity(ev.baseSeverity)
    : normalizeSeverity(ev.severity);

  if (!ev.baseSeverity) {
    if (cat === "pressure") {
      const delta = parseFloat(params.delta ?? "");
      if (!Number.isNaN(delta) && delta <= -8.0) base = "critical";
    } else if (cat === "lightning") {
      const cnt = parseInt(params.count ?? "", 10);
      if (!Number.isNaN(cnt) && cnt >= 10) base = "critical";
    } else if (cat === "space") {
      const kp = parseFloat(params.kp ?? "");
      if ((!Number.isNaN(kp) && kp >= 7.0) || params.flare === "X") base = "critical";
    }
  }

  return adjustSeverityByDistance(
    base,
    {
      source: ev.id.startsWith("dis-jma_extra-")
        ? "jma_extra"
        : ev.id.startsWith("dis-mlit_river-")
          ? "mlit_river"
          : undefined,
      category: cat,
      latitude: ev.latitude,
      longitude: ev.longitude,
    },
    config
  );
}

export function isJmaCancellationRecord(d: DisasterRecord): boolean {
  if (d.source !== "jma_extra") return false;
  const detail = (d.detail ?? "").trim();
  const idx = detail.indexOf("】");
  const body = idx >= 0 ? detail.slice(idx + 1).trim() : detail;
  if (!body) return false;
  const lines = body
    .replace(/\r\n/g, "\n")
    .replace(/。/g, "\n")
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
  const activeLines = lines.filter((l) => l.includes("解除し、") || l.includes("に切り替え") || !l.includes("解除"));
  return activeLines.length === 0;
}
