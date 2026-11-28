<script lang="ts">
  import { onMount } from "svelte";
  import * as maplibregl from "maplibre-gl";
  import "maplibre-gl/dist/maplibre-gl.css";
  import {
    appState,
    getActiveWeather,
    openLocationAI,
    queryDisasterLore,
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
  let currentRadarTileURL = "";
  let currentKikikuruTileURL = "";
  let currentHazardKind = "none";

  const hazardTileURLs: Record<string, string> = {
    flood: "https://disaportaldata.gsi.go.jp/raster/01_flood_l2_shinsuishin_data/{z}/{x}/{y}.png",
    landslide: "https://disaportaldata.gsi.go.jp/raster/05_dosekiryukeikaikuiki/{z}/{x}/{y}.png",
    avalanche: "https://disaportaldata.gsi.go.jp/raster/05_nadarekikenkasyo/{z}/{x}/{y}.png",
  };

  function syncKikikuruLayer() {
    if (!map || !mapLoaded) return;
    const kind = appState.layers.kikikuru;
    const tileInfo = kind !== "none" ? appState.kikikuru?.[kind] : undefined;
    const url = tileInfo?.tileUrlTemplate ?? "";
    const vis = kind !== "none" && url !== "" ? "visible" : "none";

    if (url !== "" && url !== currentKikikuruTileURL) {
      if (map.getLayer("kikikuru-layer")) map.removeLayer("kikikuru-layer");
      if (map.getSource("kikikuru-src")) map.removeSource("kikikuru-src");
      map.addSource("kikikuru-src", {
        type: "raster",
        tiles: [url],
        tileSize: 256,
        minzoom: 4,
        maxzoom: 12,
        bounds: [115, 18, 155, 50],
      });
      const beforeLayer = map.getLayer("isobars-line") ? "isobars-line" : undefined;
      map.addLayer(
        {
          id: "kikikuru-layer",
          type: "raster",
          source: "kikikuru-src",
          paint: { "raster-opacity": 0.72 },
        },
        beforeLayer
      );
      currentKikikuruTileURL = url;
    }
    if (map.getLayer("kikikuru-layer")) {
      map.setLayoutProperty("kikikuru-layer", "visibility", vis);
    }
  }

  function syncHazardLayer() {
    if (!map || !mapLoaded) return;
    const kind = appState.layers.hazard;
    const url = hazardTileURLs[kind] ?? "";
    const vis = kind !== "none" && url !== "" ? "visible" : "none";

    if (url !== "" && kind !== currentHazardKind) {
      if (map.getLayer("hazard-layer")) map.removeLayer("hazard-layer");
      if (map.getSource("hazard-src")) map.removeSource("hazard-src");
      map.addSource("hazard-src", {
        type: "raster",
        tiles: [url],
        tileSize: 256,
        minzoom: 2,
        maxzoom: 17,
        bounds: [122, 20, 154, 46],
      });
      const beforeLayer = map.getLayer("isobars-line") ? "isobars-line" : undefined;
      map.addLayer(
        {
          id: "hazard-layer",
          type: "raster",
          source: "hazard-src",
          paint: { "raster-opacity": 0.58 },
        },
        beforeLayer
      );
      currentHazardKind = kind;
    }
    if (map.getLayer("hazard-layer")) {
      map.setLayoutProperty("hazard-layer", "visibility", vis);
    }
  }

  function syncDisastersLayer() {
    if (!map || !mapLoaded) return;
    const ptSrc = map.getSource("disasters-pt-src") as maplibregl.GeoJSONSource | undefined;
    const polySrc = map.getSource("disasters-poly-src") as maplibregl.GeoJSONSource | undefined;
    const vis = appState.layers.disasters ? "visible" : "none";
    const items = appState.layers.disasters ? appState.disasters : [];

    if (ptSrc) {
      const features = items
        .filter((d) => Number.isFinite(d.latitude) && Number.isFinite(d.longitude) && (d.latitude !== 0 || d.longitude !== 0))
        .map((d) => ({
          type: "Feature" as const,
          properties: {
            id: d.id,
            source: d.source,
            category: d.category,
            severity: d.severity,
            title: d.title,
            detail: d.detail || "",
            alertLevel: d.alertLevel || "",
            areaName: d.areaName || "",
          },
          geometry: {
            type: "Point" as const,
            coordinates: [d.longitude, d.latitude] as [number, number],
          },
        }));
      ptSrc.setData({ type: "FeatureCollection", features });
    }
    if (polySrc) {
      const polyFeatures = items
        .filter((d) => Array.isArray(d.polygon) && d.polygon.length >= 2)
        .map((d) => {
          const pts = (d.polygon ?? []).filter((p): p is number[] => Array.isArray(p) && p.length >= 2);
          const isRing = pts.length >= 3 && d.source === "gdacs";
          return {
            type: "Feature" as const,
            properties: {
              severity: d.severity,
              title: d.title,
            },
            geometry: isRing
              ? {
                  type: "Polygon" as const,
                  coordinates: [pts.map((p) => [p[0], p[1]] as [number, number])],
                }
              : {
                  type: "LineString" as const,
                  coordinates: pts.map((p) => [p[0], p[1]] as [number, number]),
                },
          };
        });
      polySrc.setData({ type: "FeatureCollection", features: polyFeatures });
    }
    for (const lid of ["disasters-pt-circle", "disasters-poly-fill", "disasters-poly-line"]) {
      if (map.getLayer(lid)) map.setLayoutProperty(lid, "visibility", vis);
    }
  }

  function haversineKm(lat1: number, lon1: number, lat2: number, lon2: number): number {
    const rad = Math.PI / 180;
    const dLat = (lat2 - lat1) * rad;
    const dLon = (lon2 - lon1) * rad;
    const a =
      Math.sin(dLat / 2) * Math.sin(dLat / 2) +
      Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(dLon / 2) * Math.sin(dLon / 2);
    return 6371 * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  }

  function syncRiversLayer() {
    if (!map || !mapLoaded) return;
    const mode = appState.layers.rivers;
    const src = map.getSource("rivers-src") as maplibregl.GeoJSONSource | undefined;
    const vis = mode !== "off" ? "visible" : "none";
    if (src) {
      let list = mode === "off" ? [] : appState.rivers;
      if (mode === "nearby") {
        const locs = appState.config?.locations ?? [];
        const radiusKm = appState.config?.riverRadiusKm && appState.config.riverRadiusKm > 0 ? appState.config.riverRadiusKm : 30;
        list = list.filter((r) =>
          locs.some((loc) => haversineKm(loc.latitude, loc.longitude, r.latitude, r.longitude) <= radiusKm)
        );
      }
      const features = list.map((r) => ({
        type: "Feature" as const,
        properties: {
          stationId: r.stationId,
          stationName: r.stationName,
          riverName: r.riverName,
          waterLevelM: r.waterLevelM,
          warningLevelM: r.warningLevelM ?? 0,
          dangerLevelM: r.dangerLevelM ?? 0,
          status: r.status,
        },
        geometry: {
          type: "Point" as const,
          coordinates: [r.longitude, r.latitude] as [number, number],
        },
      }));
      src.setData({ type: "FeatureCollection", features });
    }
    if (map.getLayer("rivers-circle")) {
      map.setLayoutProperty("rivers-circle", "visibility", vis);
    }
  }

  async function syncLoreLayer() {
    if (!map || !mapLoaded) return;
    const vis = appState.layers.lore ? "visible" : "none";
    for (const lid of ["lore-clusters", "lore-cluster-count", "lore-unclustered"]) {
      if (map.getLayer(lid)) map.setLayoutProperty(lid, "visibility", vis);
    }
    if (!appState.layers.lore) return;
    const bounds = map.getBounds();
    const items = await queryDisasterLore({
      latitude: 0,
      longitude: 0,
      radiusKm: 0,
      minLat: bounds.getSouth(),
      maxLat: bounds.getNorth(),
      minLon: bounds.getWest(),
      maxLon: bounds.getEast(),
      limit: 800,
    });
    const src = map.getSource("lore-src") as maplibregl.GeoJSONSource | undefined;
    if (src) {
      src.setData({
        type: "FeatureCollection",
        features: items.map((l) => ({
          type: "Feature" as const,
          properties: {
            id: l.id,
            name: l.name,
            disasterKind: l.disasterKind,
            category: l.category,
            yearText: l.yearText || (l.year ? String(l.year) : ""),
            summary: l.summary || "",
          },
          geometry: {
            type: "Point" as const,
            coordinates: [l.longitude, l.latitude] as [number, number],
          },
        })),
      });
    }
  }

  function syncRadarLayer() {
    if (!map || !mapLoaded) return;
    const url = appState.radar?.tileUrlTemplate ?? "";
    const vis = appState.layers.radar && url !== "" ? "visible" : "none";

    if (url !== "") {
      const existing = map.getSource("radar-src") as maplibregl.RasterTileSource | undefined;
      if (!existing) {
        map.addSource("radar-src", {
          type: "raster",
          tiles: [url],
          tileSize: 256,
          maxzoom: 10,
          bounds: [115, 18, 155, 50],
        });
        const beforeLayer = map.getLayer("isobars-line") ? "isobars-line" : undefined;
        map.addLayer(
          {
            id: "radar-layer",
            type: "raster",
            source: "radar-src",
            paint: { "raster-opacity": 0.65 },
          },
          beforeLayer
        );
        currentRadarTileURL = url;
      } else if (url !== currentRadarTileURL && typeof existing.setTiles === "function") {
        existing.setTiles([url]);
        currentRadarTileURL = url;
      }
    }

    if (map.getLayer("radar-layer")) {
      map.setLayoutProperty("radar-layer", "visibility", vis);
    }
  }

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

    if (appState.layers.currentValues) {
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
        const precipText = w
          ? w.snowfall > 0
            ? ` ❄${w.snowfall.toFixed(1)}cm`
            : w.precipitation > 0
              ? ` 💧${w.precipitation.toFixed(1)}mm`
              : ""
          : "";
        el.innerHTML = `<span class="pin-dot"></span><span class="pin-name">${escapeHtml(label)}</span>${
          w ? `<span class="pin-meta">${tempText} ${pressText}${precipText}</span>` : ""
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
    }

    if (appState.layers.marine) {
      const mars = appState.config?.marineLocations ?? [];
      for (const ml of mars) {
        const snap = appState.marines.find((item) => item.locationId === ml.id);
        const isSelected = ml.id === appState.selectedMarineId;
        const el = document.createElement("button");
        el.type = "button";
        el.className = `marine-pin ${ml.category === "climate" ? "climate" : ""} ${isSelected ? "selected" : ""}`;
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

      map.addSource("disasters-poly-src", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
      });
      map.addSource("disasters-pt-src", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
      });
      map.addSource("rivers-src", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
      });
      map.addSource("lore-src", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
        cluster: true,
        clusterMaxZoom: 12,
        clusterRadius: 42,
      });

      map.addLayer({
        id: "disasters-poly-fill",
        type: "fill",
        source: "disasters-poly-src",
        paint: {
          "fill-color": [
            "match",
            ["get", "severity"],
            "critical", "#ef4444",
            "warning", "#f59e0b",
            "#38bdf8",
          ],
          "fill-opacity": 0.2,
        },
      });
      map.addLayer({
        id: "disasters-poly-line",
        type: "line",
        source: "disasters-poly-src",
        paint: {
          "line-color": [
            "match",
            ["get", "severity"],
            "critical", "#ef4444",
            "warning", "#f59e0b",
            "#38bdf8",
          ],
          "line-width": 2,
          "line-opacity": 0.85,
        },
      });
      map.addLayer({
        id: "disasters-pt-circle",
        type: "circle",
        source: "disasters-pt-src",
        paint: {
          "circle-radius": [
            "match",
            ["get", "severity"],
            "critical", 8,
            "warning", 6.5,
            5,
          ],
          "circle-color": [
            "match",
            ["get", "severity"],
            "critical", "#ef4444",
            "warning", "#f97316",
            "#38bdf8",
          ],
          "circle-stroke-width": 1.5,
          "circle-stroke-color": "#06070f",
        },
      });

      map.addLayer({
        id: "rivers-circle",
        type: "circle",
        source: "rivers-src",
        paint: {
          "circle-radius": 6,
          "circle-color": [
            "match",
            ["get", "status"],
            "danger", "#ef4444",
            "warning", "#f97316",
            "caution", "#eab308",
            "#14b8a6",
          ],
          "circle-stroke-width": 1.5,
          "circle-stroke-color": "#06070f",
        },
      });

      map.addLayer({
        id: "lore-clusters",
        type: "circle",
        source: "lore-src",
        filter: ["has", "point_count"],
        paint: {
          "circle-color": "#a855f7",
          "circle-radius": ["step", ["get", "point_count"], 13, 20, 17, 100, 22],
          "circle-opacity": 0.78,
          "circle-stroke-width": 1.5,
          "circle-stroke-color": "#f3e8ff",
        },
      });
      map.addLayer({
        id: "lore-cluster-count",
        type: "symbol",
        source: "lore-src",
        filter: ["has", "point_count"],
        layout: {
          "text-field": "{point_count_abbreviated}",
          "text-size": 11,
        },
        paint: { "text-color": "#ffffff" },
      });
      map.addLayer({
        id: "lore-unclustered",
        type: "circle",
        source: "lore-src",
        filter: ["!", ["has", "point_count"]],
        paint: {
          "circle-color": "#c084fc",
          "circle-radius": 5.5,
          "circle-stroke-width": 1.5,
          "circle-stroke-color": "#06070f",
        },
      });

      for (const layerId of ["disasters-pt-circle", "rivers-circle", "lore-unclustered"]) {
        map.on("mouseenter", layerId, () => {
          if (map) map.getCanvas().style.cursor = "pointer";
        });
        map.on("mouseleave", layerId, () => {
          if (map) map.getCanvas().style.cursor = "";
        });
      }

      map.on("click", "disasters-pt-circle", (e) => {
        const f = e.features?.[0];
        if (!f || !map) return;
        const p = f.properties as Record<string, string>;
        const coords = (f.geometry as GeoJSON.Point).coordinates as [number, number];
        new maplibregl.Popup({ closeButton: true, maxWidth: "280px" })
          .setLngLat(coords)
          .setHTML(
            `<div style="color:#0f172a;font-size:12px"><strong>${p.title || ""}</strong><div>${p.alertLevel || p.category || ""} (${p.source || ""})</div><div style="margin-top:4px;opacity:.85">${(p.detail || "").slice(0, 180)}</div></div>`
          )
          .addTo(map);
      });

      map.on("click", "rivers-circle", (e) => {
        const f = e.features?.[0];
        if (!f || !map) return;
        const p = f.properties as Record<string, any>;
        const coords = (f.geometry as GeoJSON.Point).coordinates as [number, number];
        new maplibregl.Popup({ closeButton: true, maxWidth: "260px" })
          .setLngLat(coords)
          .setHTML(
            `<div style="color:#0f172a;font-size:12px"><strong>${p.riverName} · ${p.stationName}</strong><div>1h雨量/水位指標: <b>${Number(p.waterLevelM).toFixed(1)} mm/h</b> (${p.status})</div><div>注意基準: ${Number(p.warningLevelM).toFixed(0)} / 危険基準: ${Number(p.dangerLevelM).toFixed(0)}</div></div>`
          )
          .addTo(map);
      });

      map.on("click", "lore-unclustered", (e) => {
        const f = e.features?.[0];
        if (!f || !map) return;
        const p = f.properties as Record<string, string>;
        const coords = (f.geometry as GeoJSON.Point).coordinates as [number, number];
        new maplibregl.Popup({ closeButton: true, maxWidth: "280px" })
          .setLngLat(coords)
          .setHTML(
            `<div style="color:#0f172a;font-size:12px"><strong>🪨 ${p.name || ""}</strong><div>${p.disasterKind || ""} ${p.yearText ? `(${p.yearText})` : ""}</div><div style="margin-top:4px;opacity:.85">${(p.summary || "").slice(0, 200)}</div></div>`
          )
          .addTo(map);
      });

      mapLoaded = true;
      syncRadarLayer();
      syncKikikuruLayer();
      syncHazardLayer();
      syncSynopticLayers();
      syncDisastersLayer();
      syncRiversLayer();
      void syncLoreLayer();
    });

    map.on("moveend", () => {
      if (appState.layers.lore) {
        void syncLoreLayer();
      }
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
    void appState.radar;
    void appState.layers.radar;
    void mapLoaded;
    syncRadarLayer();
  });

  $effect(() => {
    void appState.config?.locations;
    void appState.config?.marineLocations;
    void appState.weathers;
    void appState.marines;
    void appState.selectedLocationId;
    void appState.selectedMarineId;
    void appState.layers.marine;
    void appState.layers.currentValues;
    void i18nLangTrigger();
    rebuildMarkers();
  });

  function i18nLangTrigger() {
    return t("app.title");
  }

  $effect(() => {
    void appState.kikikuru;
    void appState.layers.kikikuru;
    void mapLoaded;
    syncKikikuruLayer();
  });

  $effect(() => {
    void appState.layers.hazard;
    void mapLoaded;
    syncHazardLayer();
  });

  $effect(() => {
    void appState.disasters;
    void appState.layers.disasters;
    void mapLoaded;
    syncDisastersLayer();
  });

  $effect(() => {
    void appState.rivers;
    void appState.layers.rivers;
    void appState.config?.locations;
    void appState.config?.riverRadiusKm;
    void mapLoaded;
    syncRiversLayer();
  });

  $effect(() => {
    void appState.loreCount;
    void appState.layers.lore;
    void mapLoaded;
    void syncLoreLayer();
  });

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
      <button class:active={appState.layers.radar} onclick={() => (appState.layers.radar = !appState.layers.radar)}>
        {t("layers.radar")}
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
      <button class:active={appState.layers.disasters} onclick={() => (appState.layers.disasters = !appState.layers.disasters)}>
        {t("layers.disasters")} ({appState.disasters.length})
      </button>
      <select class="layer-select" bind:value={appState.layers.rivers} aria-label={t("layers.rivers")}>
        <option value="off">{t("layers.rivers")}: OFF</option>
        <option value="nearby">{t("layers.riversNearby")}</option>
        <option value="all">{t("layers.riversAll")} ({appState.rivers.length})</option>
      </select>
      <button class:active={appState.layers.lore} onclick={() => (appState.layers.lore = !appState.layers.lore)}>
        {t("layers.lore")} ({appState.loreCount})
      </button>
      <select class="layer-select" bind:value={appState.layers.kikikuru} aria-label={t("layers.kikikuru")}>
        <option value="none">{t("layers.kikikuru")}: OFF</option>
        <option value="land">{t("layers.kikikuruLand")}</option>
        <option value="inund">{t("layers.kikikuruInund")}</option>
        <option value="flood">{t("layers.kikikuruFlood")}</option>
      </select>
      <select class="layer-select" bind:value={appState.layers.hazard} aria-label={t("layers.hazard")}>
        <option value="none">{t("layers.hazard")}: OFF</option>
        <option value="flood">{t("layers.hazardFlood")}</option>
        <option value="landslide">{t("layers.hazardLandslide")}</option>
        <option value="avalanche">{t("layers.hazardAvalanche")}</option>
      </select>
      <button class:active={appState.layers.currentValues} onclick={() => (appState.layers.currentValues = !appState.layers.currentValues)}>
        {t("layers.currentValues")}
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
          <button class:active={appState.layers.radar} onclick={() => (appState.layers.radar = !appState.layers.radar)}>
            {t("layers.radar")}
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
          <button class:active={appState.layers.disasters} onclick={() => (appState.layers.disasters = !appState.layers.disasters)}>
            {t("layers.disasters")} ({appState.disasters.length})
          </button>
          <button
            class:active={appState.layers.rivers !== "off"}
            onclick={() => {
              appState.layers.rivers =
                appState.layers.rivers === "off"
                  ? "nearby"
                  : appState.layers.rivers === "nearby"
                    ? "all"
                    : "off";
            }}
          >
            {appState.layers.rivers === "off"
              ? `${t("layers.rivers")}: OFF`
              : appState.layers.rivers === "nearby"
                ? t("layers.riversNearby")
                : `${t("layers.riversAll")} (${appState.rivers.length})`}
          </button>
          <button class:active={appState.layers.lore} onclick={() => (appState.layers.lore = !appState.layers.lore)}>
            {t("layers.lore")} ({appState.loreCount})
          </button>
          <button class:active={appState.layers.currentValues} onclick={() => (appState.layers.currentValues = !appState.layers.currentValues)}>
            {t("layers.currentValues")}
          </button>
          <button class:active={basemap === "osm"} onclick={() => setBasemap("osm")}>OSM</button>
          <button class:active={basemap === "gsi"} onclick={() => setBasemap("gsi")}>GSI</button>
        </div>
      </div>
    {/if}
  {/if}

  {#if appState.layers.currentValues && activeWeather}
    <div class="hud-card">
      <div class="hud-head">
        <div class="hud-loc">{activeLocationName}</div>
        <button
          type="button"
          class="hud-ai-btn"
          title={t("ai.explainLocationBtn")}
          onclick={() => openLocationAI(activeWeather.locationId)}
        >
          ✨ {t("ai.explainLocationBtn")}
        </button>
      </div>
      <div>
        <strong>{activeWeather.pressureMsl.toFixed(1)}</strong> hPa
        <span class="delta" class:drop={activeWeather.pressureDelta3h <= -2}>
          ({activeWeather.pressureDelta3h >= 0 ? "+" : ""}{activeWeather.pressureDelta3h.toFixed(1)} / 3h)
        </span>
      </div>
      <div>{activeWeather.temperature2m.toFixed(1)} °C / CAPE {activeWeather.cape.toFixed(0)}</div>
      {#if activeWeather.precipitation > 0 || activeWeather.snowfall > 0 || activeWeather.snowDepth > 0}
        <div>
          {#if activeWeather.precipitation > 0}💧 {activeWeather.precipitation.toFixed(1)}mm{/if}
          {#if activeWeather.snowfall > 0} ❄ {activeWeather.snowfall.toFixed(1)}cm{/if}
          {#if activeWeather.snowDepth > 0} ({t("metrics.snowDepth")} {(activeWeather.snowDepth * 100).toFixed(0)}cm){/if}
        </div>
      {/if}
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
  .layer-bar .layer-select,
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
  .hud-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .hud-ai-btn {
    background: rgba(14, 116, 144, 0.3);
    border: 1px solid rgba(56, 189, 248, 0.45);
    color: #bae6fd;
    border-radius: 5px;
    padding: 2px 6px;
    font-size: 0.68rem;
    cursor: pointer;
  }
  .hud-ai-btn:hover {
    border-color: #38bdf8;
    color: #ffffff;
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
  :global(.marine-pin.climate) {
    background: rgba(66, 32, 6, 0.88);
    color: #fde68a;
    border-color: rgba(245, 158, 11, 0.5);
  }
  :global(.marine-pin.climate.selected) {
    border-color: #fbbf24;
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.45);
  }
  :global(.marine-pin .m-dot) {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #2dd4bf;
  }
  :global(.marine-pin.climate .m-dot) {
    background: #fbbf24;
  }
</style>
