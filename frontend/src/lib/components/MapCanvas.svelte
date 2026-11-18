<script lang="ts">
  import { onMount } from "svelte";
  import maplibregl from "maplibre-gl";
  import "maplibre-gl/dist/maplibre-gl.css";
  import { appState } from "../stores/appState.svelte";
  import { createJetStreamOverlay } from "./JetStreamP5";

  let mapEl: HTMLDivElement;
  let p5El: HTMLDivElement;
  let map: maplibregl.Map | null = null;
  let homeMarker: maplibregl.Marker | null = null;

  let showJetStream = $state(true);
  let showQuakes = $state(true);
  let showLightning = $state(true);
  let showVolcanoes = $state(true);
  let basemap = $state<"osm" | "gsi">("osm");

  function setBasemap(next: "osm" | "gsi") {
    basemap = next;
    if (!map) return;
    map.setLayoutProperty("osm-layer", "visibility", next === "osm" ? "visible" : "none");
    map.setLayoutProperty("gsi-layer", "visibility", next === "gsi" ? "visible" : "none");
  }

  onMount(() => {
    const centerLon = appState.config?.longitude ?? 139.5492;
    const centerLat = appState.config?.latitude ?? 35.8569;

    map = new maplibregl.Map({
      container: mapEl,
      center: [centerLon, centerLat],
      zoom: 5.2,
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

    homeMarker = new maplibregl.Marker({ color: "#38bdf8" })
      .setLngLat([centerLon, centerLat])
      .addTo(map);

    const destroyP5 = createJetStreamOverlay(p5El, map, () => ({
      windGrid: appState.windGrid,
      quakes: appState.quakes,
      strikes: appState.liden?.strikes ?? [],
      volcanoes: appState.volcanoes,
      showJetStream,
      showQuakes,
      showLightning,
      showVolcanoes,
      lightMode: appState.platform.isMobile || Boolean(appState.config?.mobileLightMode),
    }));

    return () => {
      destroyP5();
      map?.remove();
      map = null;
    };
  });

  $effect(() => {
    if (map && homeMarker && appState.config) {
      homeMarker.setLngLat([appState.config.longitude, appState.config.latitude]);
    }
  });

  $effect(() => {
    if (map && appState.focusCoord) {
      map.flyTo({
        center: [appState.focusCoord.lon, appState.focusCoord.lat],
        zoom: appState.focusCoord.zoom ?? 7,
        duration: 900,
      });
    }
  });
</script>

<div class="map-wrap">
  <div class="map-canvas" bind:this={mapEl}></div>
  <div class="p5-overlay" bind:this={p5El}></div>

  <div class="layer-bar">
    <button class:active={showJetStream} onclick={() => (showJetStream = !showJetStream)}>偏西風 250hPa</button>
    <button class:active={showQuakes} onclick={() => (showQuakes = !showQuakes)}>地震 ({appState.quakes.length})</button>
    <button class:active={showLightning} onclick={() => (showLightning = !showLightning)}>
      落雷 ({appState.liden?.nearbyCount ?? 0}/{appState.liden?.totalCount ?? 0})
    </button>
    <button class:active={showVolcanoes} onclick={() => (showVolcanoes = !showVolcanoes)}>火山</button>
    <button class:active={basemap === "osm"} onclick={() => setBasemap("osm")}>OSM</button>
    <button class:active={basemap === "gsi"} onclick={() => setBasemap("gsi")}>国土地理院</button>
  </div>

  {#if appState.weather}
    <div class="hud-card">
      <div><strong>{appState.weather.pressureMsl.toFixed(1)}</strong> hPa</div>
      <div>{appState.weather.temperature2m.toFixed(1)} °C / CAPE {appState.weather.cape.toFixed(0)}</div>
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
    z-index: 10;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .layer-bar button {
    background: rgba(13, 18, 34, 0.85);
    color: #94a3b8;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 5px 9px;
    font-size: 0.75rem;
    cursor: pointer;
    backdrop-filter: blur(6px);
  }
  .layer-bar button.active {
    color: #38bdf8;
    border-color: rgba(56, 189, 248, 0.6);
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
    font-size: 0.78rem;
    color: #e2e8f0;
    backdrop-filter: blur(6px);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
</style>
