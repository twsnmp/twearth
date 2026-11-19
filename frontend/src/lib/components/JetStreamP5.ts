import p5 from "p5";
import type maplibregl from "maplibre-gl";
import type {
  LightningStrike,
  QuakeEvent,
  VolcanoInfo,
  WindGridPoint,
} from "../../../bindings/github.com/twsnmp/twearth/internal/collector/models";

export interface OverlayState {
  windGrid: WindGridPoint[];
  quakes: QuakeEvent[];
  strikes: LightningStrike[];
  volcanoes: VolcanoInfo[];
  showJetStream: boolean;
  showQuakes: boolean;
  showLightning: boolean;
  showVolcanoes: boolean;
  lightMode: boolean;
}

interface Particle {
  lon: number;
  lat: number;
  age: number;
  maxAge: number;
}

const GRID_MIN_LAT = 20;
const GRID_MAX_LAT = 50;
const GRID_MIN_LON = 120;
const GRID_MAX_LON = 155;
const GRID_STEP = 5;

function isValidLngLat(lon: number, lat: number): boolean {
  return (
    Number.isFinite(lon) &&
    Number.isFinite(lat) &&
    lat >= -90 &&
    lat <= 90 &&
    lon >= -180 &&
    lon <= 180 &&
    !(lat === 0 && lon === 0)
  );
}

export function createJetStreamOverlay(
  container: HTMLElement,
  map: maplibregl.Map,
  getState: () => OverlayState
): () => void {
  let gridIndex = new Map<string, WindGridPoint>();
  let lastGridRef: WindGridPoint[] = [];

  function rebuildGridIndex(grid: WindGridPoint[]) {
    if (grid === lastGridRef) return;
    lastGridRef = grid;
    gridIndex = new Map();
    for (const pt of grid) {
      const key = `${Math.round(pt.latitude)}_${Math.round(pt.longitude)}`;
      gridIndex.set(key, pt);
    }
  }

  function sampleWind250(lon: number, lat: number): { u: number; v: number; speed: number } | null {
    if (gridIndex.size === 0) return null;
    const lat0 = Math.floor(lat / GRID_STEP) * GRID_STEP;
    const lon0 = Math.floor(lon / GRID_STEP) * GRID_STEP;
    const lat1 = lat0 + GRID_STEP;
    const lon1 = lon0 + GRID_STEP;

    const p00 = gridIndex.get(`${lat0}_${lon0}`);
    const p10 = gridIndex.get(`${lat0}_${lon1}`);
    const p01 = gridIndex.get(`${lat1}_${lon0}`);
    const p11 = gridIndex.get(`${lat1}_${lon1}`);
    if (!p00 && !p10 && !p01 && !p11) return null;

    const tx = (lon - lon0) / GRID_STEP;
    const ty = (lat - lat0) / GRID_STEP;

    const toUV = (pt: WindGridPoint | undefined) => {
      if (!pt) return { u: 0, v: 0, s: 0 };
      const rad = ((pt.windDirection250hPa + 180) * Math.PI) / 180;
      return {
        u: Math.sin(rad) * pt.windSpeed250hPa,
        v: Math.cos(rad) * pt.windSpeed250hPa,
        s: pt.windSpeed250hPa,
      };
    };

    const a = toUV(p00 ?? p10 ?? p01 ?? p11);
    const b = toUV(p10 ?? p00 ?? p11 ?? p01);
    const c = toUV(p01 ?? p11 ?? p00 ?? p10);
    const d = toUV(p11 ?? p01 ?? p10 ?? p00);

    const u = (1 - tx) * (1 - ty) * a.u + tx * (1 - ty) * b.u + (1 - tx) * ty * c.u + tx * ty * d.u;
    const v = (1 - tx) * (1 - ty) * a.v + tx * (1 - ty) * b.v + (1 - tx) * ty * c.v + tx * ty * d.v;
    const speed = Math.hypot(u, v);
    return { u, v, speed };
  }

  const sketch = (s: p5) => {
    const particles: Particle[] = [];

    function resetParticle(p: Particle) {
      p.lon = GRID_MIN_LON + Math.random() * (GRID_MAX_LON - GRID_MIN_LON);
      p.lat = GRID_MIN_LAT + Math.random() * (GRID_MAX_LAT - GRID_MIN_LAT);
      p.age = 0;
      p.maxAge = 45 + Math.floor(Math.random() * 75);
    }

    function syncParticleCount(target: number) {
      while (particles.length < target) {
        const p: Particle = { lon: 135, lat: 35, age: 0, maxAge: 80 };
        resetParticle(p);
        particles.push(p);
      }
      if (particles.length > target) {
        particles.length = target;
      }
    }

    s.setup = () => {
      const rect = container.getBoundingClientRect();
      s.createCanvas(Math.max(1, rect.width), Math.max(1, rect.height));
      s.frameRate(getState().lightMode ? 30 : 45);
    };

    s.windowResized = () => {
      const rect = container.getBoundingClientRect();
      s.resizeCanvas(Math.max(1, rect.width), Math.max(1, rect.height));
    };

    s.draw = () => {
      try {
        const st = getState();
        s.clear();

        if (st.showJetStream && st.windGrid.length > 0) {
          rebuildGridIndex(st.windGrid);
          syncParticleCount(st.lightMode ? 450 : 1200);

          s.strokeWeight(st.lightMode ? 1.4 : 1.8);
          for (const p of particles) {
            p.age++;
            if (p.age > p.maxAge) {
              resetParticle(p);
              continue;
            }
            const w = sampleWind250(p.lon, p.lat);
            if (!w || !Number.isFinite(w.u) || !Number.isFinite(w.v) || !Number.isFinite(w.speed) || w.speed < 1) {
              resetParticle(p);
              continue;
            }
            if (!isValidLngLat(p.lon, p.lat)) {
              resetParticle(p);
              continue;
            }

            const prev = map.project([p.lon, p.lat]);
            const scale = 0.0045;
            p.lon += w.u * scale;
            p.lat += w.v * scale;

            if (
              !isValidLngLat(p.lon, p.lat) ||
              p.lon < GRID_MIN_LON ||
              p.lon > GRID_MAX_LON ||
              p.lat < GRID_MIN_LAT ||
              p.lat > GRID_MAX_LAT
            ) {
              resetParticle(p);
              continue;
            }

            const next = map.project([p.lon, p.lat]);
            const [r, g, b] = speedToRGB(w.speed);
            const alpha = Math.sin((p.age / p.maxAge) * Math.PI) * 190;
            s.stroke(r, g, b, alpha);
            s.line(prev.x, prev.y, next.x, next.y);
          }
        }

        const nowMs = Date.now();
        if (st.showQuakes) {
          for (const q of st.quakes.slice(0, 25)) {
            if (!isValidLngLat(q.longitude, q.latitude)) continue;
            const pt = map.project([q.longitude, q.latitude]);
            const mag = Number.isFinite(q.magnitude) && q.magnitude > 0 ? q.magnitude : 1;
            const phase = ((nowMs / 1000 + mag) % 2.5) / 2.5;
            const maxR = Math.max(12, Math.min(70, mag * 10));
            const rad = phase * maxR;
            const alpha = (1 - phase) * 200;
            s.noFill();
            s.strokeWeight(2);
            if (mag >= 5.5) {
              s.stroke(248, 113, 113, alpha);
            } else if (mag >= 4.0) {
              s.stroke(251, 191, 36, alpha);
            } else {
              s.stroke(56, 189, 248, alpha);
            }
            s.circle(pt.x, pt.y, rad * 2);
          }
        }

        if (st.showLightning) {
          const flash = 0.5 + 0.5 * Math.sin(nowMs / 140);
          s.noStroke();
          for (const l of st.strikes.slice(0, st.lightMode ? 120 : 300)) {
            if (!isValidLngLat(l.longitude, l.latitude)) continue;
            const pt = map.project([l.longitude, l.latitude]);
            s.fill(253, 224, 71, 140 + flash * 115);
            s.circle(pt.x, pt.y, 4 + flash * 3);
          }
        }

        if (st.showVolcanoes) {
          for (const v of st.volcanoes) {
            if (v.alertLevel < 2) continue;
            if (!isValidLngLat(v.longitude, v.latitude)) continue;
            const pt = map.project([v.longitude, v.latitude]);
            const pulse = (nowMs / 800) % 1;
            s.noFill();
            s.stroke(249, 115, 22, (1 - pulse) * 210);
            s.strokeWeight(2);
            s.circle(pt.x, pt.y, 14 + pulse * 24);
          }
        }
      } catch (err) {
        console.error("JetStreamP5 draw error:", err);
      }
    };
  };

  const instance = new p5(sketch, container);
  const redraw = () => instance.redraw();
  const onResize = () => {
    const rect = container.getBoundingClientRect();
    instance.resizeCanvas(Math.max(1, rect.width), Math.max(1, rect.height));
  };
  map.on("move", redraw);
  map.on("zoom", redraw);
  map.on("resize", onResize);

  return () => {
    map.off("move", redraw);
    map.off("zoom", redraw);
    map.off("resize", onResize);
    instance.remove();
  };
}

function speedToRGB(speedKmh: number): [number, number, number] {
  if (speedKmh >= 220) return [192, 132, 252]; // purple
  if (speedKmh >= 160) return [248, 113, 113]; // red
  if (speedKmh >= 110) return [250, 204, 21]; // yellow
  if (speedKmh >= 70) return [74, 222, 128]; // green
  return [56, 189, 248]; // cyan-blue
}
