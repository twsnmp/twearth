<script lang="ts">
  import { onMount } from "svelte";
  import maplibregl from "maplibre-gl";
  import "maplibre-gl/dist/maplibre-gl.css";
  import {
    appState,
    getActiveWeather,
    selectLocation,
    selectMarineLocation,
  } from "../stores/appState.svelte";
  import { locDisplayName, t } from "../i18n/index.svelte";
  import { createJetStreamOverlay } from "./JetStreamP5";

  let mapEl: HTMLDivElement;
  let p5El: HTMLDivElement;
  let map: maplibregl.Map | null = null;
  let mapLoaded = $state(false);
  let cityMarkers: maplibregl.Marker[] = [];
  let marineMarkers: maplibregl.Marker[] = [];
  let basemap = $state<"osm" | "gsi">("osm");
  let mobileLayersOpen = $state(false);

  function setBasemap(next: "osm" | "gsi") {
    basemap = next;
    if (!map || !mapLoaded) return;
    map.setLayoutProperty("osm-layer", "visibility", next === "osm" ? "visible" : "none");
    map.setLayoutProperty("gsi-layer", "visibility", next === "gsi" ? "visible" : "none");
  }

  function syncSynopticLayers() {
    if (!map || !mapLoaded) return;
    const isobarSrc = map.getSource("isobars-src") as maplibregl.GeoJSONSource | undefined;
    const centerSrc = map.getSource("centers-src") as maplibregl.GeoJSONSource | undefined;
    const syn = appState.synoptic;

    if (isobarSrc) {
      const features = (syn?.isobars ?? [])
        .filter((line) => line.coordinates && line.coordinates.length >= 2)
        .map((line) => ({
          type: "Feature" as const,
          properties: {
            pressure: Math.round(line.pressure),
            isMajor: line.isMajor,
          },
          geometry: {
            type: "LineString" as const,
            coordinates: line.coordinates as [number, number][],
          },
        }));
      isobarSrc.setData({ type: "FeatureCollection", features });
    }

    if (centerSrc) {
      const features = (syn?.centers ?? []).map((c) => ({
        type: "Feature" as const,
        properties: {
          kind: c.kind,
          label: `${c.kind}\n${Math.round(c.pressure)}`,
        },
        geometry: {
          type: "Point" as const,
          coordinates: [c.longitude, c.latitude],
        },
      }));
      centerSrc.setData({ type: "FeatureCollection", features });
    }

    const vis = appState.layers.isobars ? "visible" : "none";
    for (const layerId of ["isobars-line", "isobars-label", "centers-label"]) {
      if (map.getLayer(layerId)) {
        map.setLayoutProperty(layerId, "visibility", vis);
      }
    }
  }

  function rebuildMarkers() {
    if (!map) return;
    for (const m of cityMarkers) m.remove();
    cityMarkers = [];
    for (const m of marineMarkers) m.remove();
    marineMarkers = [];

    const locs = appState.config?.locations ?? [];
    for (const loc of locs) {
      const w = appState.weathers.find((item) => item.locationId === loc.id);
      const isSelected = loc.id === appState.selectedLocationId;

      const el = document.createElement("button");
      el.type = "button";
      el.className = `city-pin ${isSelected ? "selected" : ""}`;
      const label = locDisplayName(loc);
      const tempText = w ? `${w.temperature2m.toFixed(1)}°` : "";
      const pressText = w ? `${w.pressureMsl.toFixed(0)}hPa` : "";
      el.innerHTML = `<span class="pin-dot"></span><span class="pin-name">${escapeHtml(label)}</span>${
        w ? `<span class="pin-meta">${tempText} ${pressText}</span>` : ""
      }`;
      el.addEventListener("click", (ev) => {
        ev.stopPropagation();
        selectLocation(loc.id, true);
      });

      const marker = new maplibregl.Marker({ element: el, anchor: "bottom" })
        .setLngLat([loc.longitude, loc.latitude])
        .addTo(map);
      cityMarkers.push(marker);
    }

    if (appState.layers.marine) {
      const mars = appState.config?.marineLocations ?? [];
      for (const ml of mars) {
        const snap = appState.marines.find((item) => item.locationId === ml.id);
        const isSelected = ml.id === appState.selectedMarineId;
        const el = document.createElement("button");
        el.type = "button";
        el.className = `marine-pin ${isSelected ? "selected" : ""}`;
        const label = locDisplayName(ml);
        const sstText = snap ? `${snap.seaSurfaceTemperature.toFixed(1)}°C` : "SST";
        el.innerHTML = `<span class="m-dot"></span><span class="m-name">${escapeHtml(label)} · ${sstText}</span>`;
        el.addEventListener("click", (ev) => {
          ev.stopPropagation();
          selectMarineLocation(ml.id, true);
        });
        const marker = new maplibregl.Marker({ element: el, anchor: "bottom" })
          .setLngLat([ml.longitude, ml.latitude])
          .addTo(map);
        marineMarkers.push(marker);
      }
    }
  }

  function escapeHtml(str: string): string {
    return str.replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch] ?? ch));
  }

  onMount(() => {
    const firstLoc = appState.config?.locations?.[0];
    const centerLon = firstLoc?.longitude ?? 139.6503;
    const centerLat = firstLoc?.latitude ?? 35.6762;

    map = new maplibregl.Map({
      container: mapEl,
      center: [centerLon, centerLat],
      zoom: 4.8,
      attributionControl: false,
      style: {
        version: 8,
        sources: {
          osm: {
            type: "raster",
            tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
            tileSize: 256,
            maxzoom: 19,
          },
          gsi: {
            type: "raster",
            tiles: ["https://cyberjapandata.gsi.go.jp/xyz/pale/{z}/{x}/{y}.png"],
            tileSize: 256,
            maxzoom: 18,
          },
        },
        layers: [
          {
            id: "osm-layer",
            type: "raster",
            source: "osm",
            layout: { visibility: "visible" },
          },
          {
            id: "gsi-layer",
            type: "raster",
            source: "gsi",
            layout: { visibility: "none" },
          },
        ],
      },
    });

    map.on("load", () => {
      if (!map) return;
      map.addSource("isobars-src", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
      });
      map.addSource("centers-src", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
      });

      map.addLayer({
        id: "isobars-line",
        type: "line",
        source: "isobars-src",
        paint: {
          "line-color": ["case", ["get", "isMajor"], "#38bdf8", "#7dd3fc"],
          "line-width": ["case", ["get", "isMajor"], 2.0, 1.0],
          "line-opacity": 0.72,
        },
      });

      map.addLayer({
        id: "isobars-label",
        type: "symbol",
        source: "isobars-src",
        layout: {
          "symbol-placement": "line",
          "text-field": ["concat", ["to-string", ["get", "pressure"]], " hPa"],
          "text-size": 10,
          "text-font": ["Open Sans Regular", "Arial Unicode MS Regular"],
        },
        paint: {
          "text-color": "#bae6fd",
          "text-halo-color": "#06070f",
          "text-halo-width": 1.4,
        },
      });

      map.addLayer({
        id: "centers-label",
        type: "symbol",
        source: "centers-src",
        layout: {
          "text-field": ["get", "label"],
          "text-size": 14,
          "text-allow-overlap": true,
        },
        paint: {
          "text-color": ["case", ["==", ["get", "kind"], "H"], "#38bdf8", "#f43f5e"],
          "text-halo-color": "#06070f",
          "text-halo-width": 2.0,
        },
      });

      mapLoaded = true;
      syncSynopticLayers();
    });

    map.on("click", (e) => {
      appState.pickedCoord = {
        latitude: Number(e.lngLat.lat.toFixed(4)),
        longitude: Number(e.lngLat.lng.toFixed(4)),
      };
    });

    const destroyP5 = createJetStreamOverlay(p5El, map, () => ({
      windGrid: appState.windGrid,
      quakes: appState.quakes,
      strikes: appState.liden?.strikes ?? [],
      volcanoes: appState.volcanoes,
      showJetStream: appState.layers.jetstream,
      showQuakes: appState.layers.quakes,
      showLightning: appState.layers.lightning,
      showVolcanoes: appState.layers.volcanoes,
      lightMode: appState.platform.isMobile || Boolean(appState.config?.mobileLightMode),
    }));

    return () => {
      for (const m of cityMarkers) m.remove();
      for (const m of marineMarkers) m.remove();
      destroyP5();
      map?.remove();
      map = null;
    };
  });

  $effect(() => {
    void appState.synoptic;
    void appState.layers.isobars;
    void mapLoaded;
    syncSynopticLayers();
  });

  $effect(() => {
    void appState.config?.locations;
    void appState.config?.marineLocations;
    void appState.weathers;
    void appState.marines;
    void appState.selectedLocationId;
    void appState.selectedMarineId;
    void appState.layers.marine;
    void i18nLangTrigger();
    rebuildMarkers();
  });

  function i18nLangTrigger() {
    return t("app.title");
  }

  $effect(() => {
    if (map && appState.focusCoord) {
      const { lon, lat, zoom } = appState.focusCoord;
      if (
        Number.isFinite(lon) &&
        Number.isFinite(lat) &&
        lat >= -90 &&
        lat <= 90 &&
        lon >= -180 &&
        lon <= 180
      ) {
        map.flyTo({
          center: [lon, lat],
          zoom: zoom ?? 6.8,
          duration: 850,
        });
      }
    }
  });

  const activeWeather = $derived(getActiveWeather());
  const activeLocationName = $derived.by(() => {
    if (!activeWeather) return "";
    const loc = appState.config?.locations?.find((l) => l.id === activeWeather.locationId);
    return loc ? locDisplayName(loc) : activeWeather.locationName;
  });
</script>

<div class="map-wrap">
  <div class="map-canvas" bind:this={mapEl}></div>
  <div class="p5-overlay" bind:this={p5El}></div>

  {#if !appState.platform.isMobile}
    <div class="layer-bar">
      <button class:active={appState.layers.isobars} onclick={() => (appState.layers.isobars = !appState.layers.isobars)}>
        {t("layers.isobars")}
      </button>
      <button class:active={appState.layers.jetstream} onclick={() => (appState.layers.jetstream = !appState.layers.jetstream)}>
        {t("layers.jetstream")}
      </button>
      <button class:active={appState.layers.marine} onclick={() => (appState.layers.marine = !appState.layers.marine)}>
        {t("layers.marine")}
      </button>
      <button class:active={appState.layers.quakes} onclick={() => (appState.layers.quakes = !appState.layers.quakes)}>
        {t("layers.quakes")} ({appState.quakes.length})
      </button>
      <button class:active={appState.layers.lightning} onclick={() => (appState.layers.lightning = !appState.layers.lightning)}>
        {t("layers.lightning")} ({appState.liden?.nearbyCount ?? 0}/{appState.liden?.totalCount ?? 0})
      </button>
      <button class:active={appState.layers.volcanoes} onclick={() => (appState.layers.volcanoes = !appState.layers.volcanoes)}>
        {t("layers.volcanoes")}
      </button>
      <button class:active={basemap === "osm"} onclick={() => setBasemap("osm")}>OSM</button>
      <button class:active={basemap === "gsi"} onclick={() => setBasemap("gsi")}>GSI</button>
    </div>
  {:else}
    <button class="mobile-layer-fab" onclick={() => (mobileLayersOpen = !mobileLayersOpen)}>
      {t("layers.title")}
    </button>
    {#if mobileLayersOpen}
      <div class="mobile-layer-sheet">
        <div class="sheet-head">
          <span>{t("layers.title")}</span>
          <button onclick={() => (mobileLayersOpen = false)}>✕</button>
        </div>
        <div class="sheet-grid">
          <button class:active={appState.layers.isobars} onclick={() => (appState.layers.isobars = !appState.layers.isobars)}>
            {t("layers.isobars")}
          </button>
          <button class:active={appState.layers.jetstream} onclick={() => (appState.layers.jetstream = !appState.layers.jetstream)}>
            {t("layers.jetstream")}
          </button>
          <button class:active={appState.layers.marine} onclick={() => (appState.layers.marine = !appState.layers.marine)}>
            {t("layers.marine")}
          </button>
          <button class:active={appState.layers.quakes} onclick={() => (appState.layers.quakes = !appState.layers.quakes)}>
            {t("layers.quakes")} ({appState.quakes.length})
          </button>
          <button class:active={appState.layers.lightning} onclick={() => (appState.layers.lightning = !appState.layers.lightning)}>
            {t("layers.lightning")} ({appState.liden?.nearbyCount ?? 0})
          </button>
          <button class:active={appState.layers.volcanoes} onclick={() => (appState.layers.volcanoes = !appState.layers.volcanoes)}>
            {t("layers.volcanoes")}
          </button>
          <button class:active={basemap === "osm"} onclick={() => setBasemap("osm")}>OSM</button>
          <button class:active={basemap === "gsi"} onclick={() => setBasemap("gsi")}>GSI</button>
        </div>
      </div>
    {/if}
  {/if}

  {#if activeWeather}
    <div class="hud-card">
      <div class="hud-loc">{activeLocationName}</div>
      <div>
        <strong>{activeWeather.pressureMsl.toFixed(1)}</strong> hPa
        <span class="delta" class:drop={activeWeather.pressureDelta3h <= -2}>
          ({activeWeather.pressureDelta3h >= 0 ? "+" : ""}{activeWeather.pressureDelta3h.toFixed(1)} / 3h)
        </span>
      </div>
      <div>{activeWeather.temperature2m.toFixed(1)} °C / CAPE {activeWeather.cape.toFixed(0)}</div>
      {#if appState.space}
        <div>Kp {appState.space.kpIndex.toFixed(1)} / Flare {appState.space.flareClass}</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .map-wrap {
    position: relative;
    width: 100%;
    height: 100%;
    min-height: 280px;
    overflow: hidden;
  }
  .map-canvas,
  .p5-overlay {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
  }
  .map-canvas :global(.maplibregl-canvas) {
    filter: invert(92%) hue-rotate(180deg) brightness(86%) contrast(94%);
  }
  .p5-overlay {
    pointer-events: none;
    z-index: 5;
  }
  .layer-bar {
    position: absolute;
    top: 10px;
    left: 10px;
    right: 10px;
    z-index: 10;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .layer-bar button,
  .sheet-grid button {
    background: rgba(13, 18, 34, 0.85);
    color: #94a3b8;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 5px 9px;
    font-size: 0.74rem;
    cursor: pointer;
    backdrop-filter: blur(6px);
  }
  .layer-bar button.active,
  .sheet-grid button.active {
    color: #38bdf8;
    border-color: rgba(56, 189, 248, 0.65);
    background: rgba(14, 116, 144, 0.22);
  }
  .mobile-layer-fab {
    position: absolute;
    right: 12px;
    bottom: 14px;
    z-index: 11;
    background: rgba(15, 23, 42, 0.9);
    color: #38bdf8;
    border: 1px solid rgba(56, 189, 248, 0.45);
    border-radius: 999px;
    padding: 8px 14px;
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
    backdrop-filter: blur(8px);
  }
  .mobile-layer-sheet {
    position: absolute;
    left: 10px;
    right: 10px;
    bottom: 54px;
    z-index: 12;
    background: rgba(11, 16, 32, 0.95);
    border: 1px solid rgba(56, 189, 248, 0.35);
    border-radius: 12px;
    padding: 12px;
    backdrop-filter: blur(10px);
  }
  .sheet-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
    font-size: 0.82rem;
    font-weight: 600;
    color: #e2e8f0;
  }
  .sheet-head button {
    background: transparent;
    border: none;
    color: #94a3b8;
    font-size: 1rem;
    cursor: pointer;
  }
  .sheet-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }
  .hud-card {
    position: absolute;
    bottom: 12px;
    left: 12px;
    z-index: 10;
    background: rgba(13, 18, 34, 0.85);
    border: 1px solid rgba(56, 189, 248, 0.28);
    border-radius: 8px;
    padding: 8px 12px;
    font-size: 0.76rem;
    color: #e2e8f0;
    backdrop-filter: blur(6px);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .hud-loc {
    font-weight: 600;
    color: #38bdf8;
  }
  .delta {
    font-size: 0.7rem;
    color: #94a3b8;
  }
  .delta.drop {
    color: #f43f5e;
  }
  :global(.city-pin) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: rgba(15, 23, 42, 0.88);
    color: #e2e8f0;
    border: 1px solid rgba(148, 163, 184, 0.45);
    border-radius: 999px;
    padding: 3px 8px;
    font-size: 0.7rem;
    cursor: pointer;
    white-space: nowrap;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
  }
  :global(.city-pin.selected) {
    border-color: #38bdf8;
    background: rgba(14, 116, 144, 0.92);
    color: #ffffff;
  }
  :global(.city-pin .pin-dot) {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #38bdf8;
  }
  :global(.city-pin .pin-meta) {
    color: #bae6fd;
    font-size: 0.66rem;
  }
  :global(.marine-pin) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: rgba(6, 40, 61, 0.88);
    color: #99f6e4;
    border: 1px solid rgba(45, 212, 191, 0.45);
    border-radius: 6px;
    padding: 2px 7px;
    font-size: 0.68rem;
    cursor: pointer;
    white-space: nowrap;
  }
  :global(.marine-pin.selected) {
    border-color: #2dd4bf;
    box-shadow: 0 0 8px rgba(45, 212, 191, 0.45);
  }
  :global(.marine-pin .m-dot) {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #2dd4bf;
  }
</style>
