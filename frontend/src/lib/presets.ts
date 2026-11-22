import type { MarineLocation, ObservationLocation } from "../../bindings/github.com/twsnmp/twearth/internal/store/models";

export interface CityPreset {
  id: string;
  name: string;
  nameEn: string;
  latitude: number;
  longitude: number;
  group: "japan" | "global";
}

export interface MarinePreset {
  id: string;
  name: string;
  nameEn: string;
  latitude: number;
  longitude: number;
  region: string;
  category?: "nearshore" | "regional" | "climate";
}

export const CITY_PRESETS: CityPreset[] = [
  { id: "tokyo", name: "東京", nameEn: "Tokyo", latitude: 35.6762, longitude: 139.6503, group: "japan" },
  { id: "osaka", name: "大阪", nameEn: "Osaka", latitude: 34.6937, longitude: 135.5023, group: "japan" },
  { id: "nagoya", name: "名古屋", nameEn: "Nagoya", latitude: 35.1815, longitude: 136.9066, group: "japan" },
  { id: "sapporo", name: "札幌", nameEn: "Sapporo", latitude: 43.0618, longitude: 141.3545, group: "japan" },
  { id: "sendai", name: "仙台", nameEn: "Sendai", latitude: 38.2682, longitude: 140.8694, group: "japan" },
  { id: "fukuoka", name: "福岡", nameEn: "Fukuoka", latitude: 33.5904, longitude: 130.4017, group: "japan" },
  { id: "hiroshima", name: "広島", nameEn: "Hiroshima", latitude: 34.3853, longitude: 132.4553, group: "japan" },
  { id: "naha", name: "那覇", nameEn: "Naha", latitude: 26.2124, longitude: 127.6809, group: "japan" },
  { id: "new-york", name: "ニューヨーク", nameEn: "New York", latitude: 40.7128, longitude: -74.006, group: "global" },
  { id: "london", name: "ロンドン", nameEn: "London", latitude: 51.5072, longitude: -0.1276, group: "global" },
  { id: "paris", name: "パリ", nameEn: "Paris", latitude: 48.8566, longitude: 2.3522, group: "global" },
  { id: "singapore", name: "シンガポール", nameEn: "Singapore", latitude: 1.3521, longitude: 103.8198, group: "global" },
  { id: "sydney", name: "シドニー", nameEn: "Sydney", latitude: -33.8688, longitude: 151.2093, group: "global" },
  { id: "honolulu", name: "ホノルル", nameEn: "Honolulu", latitude: 21.3099, longitude: -157.8581, group: "global" },
];

export const MARINE_PRESETS: MarinePreset[] = [
  { id: "tokyo-bay", name: "東京湾", nameEn: "Tokyo Bay", latitude: 35.48, longitude: 139.82, region: "Pacific", category: "regional" },
  { id: "sagami-bay", name: "相模湾", nameEn: "Sagami Bay", latitude: 35.15, longitude: 139.35, region: "Pacific", category: "regional" },
  { id: "suruga-bay", name: "駿河湾", nameEn: "Suruga Bay", latitude: 34.9, longitude: 138.55, region: "Pacific", category: "regional" },
  { id: "enshu-nada", name: "遠州灘", nameEn: "Enshu-nada", latitude: 34.35, longitude: 137.4, region: "Pacific", category: "regional" },
  { id: "kii-channel", name: "紀伊水道", nameEn: "Kii Channel", latitude: 33.85, longitude: 134.9, region: "Pacific", category: "regional" },
  { id: "kuroshio-shionomisaki", name: "潮岬沖 (黒潮域)", nameEn: "Off Shionomisaki (Kuroshio)", latitude: 33.25, longitude: 135.75, region: "Pacific", category: "regional" },
  { id: "hyuga-nada", name: "日向灘", nameEn: "Hyuga-nada", latitude: 32.0, longitude: 131.85, region: "Pacific", category: "regional" },
  { id: "sendai-bay", name: "仙台湾", nameEn: "Sendai Bay", latitude: 38.05, longitude: 141.4, region: "Pacific", category: "regional" },
  { id: "oyashio-sanriku", name: "三陸沖 (親潮域)", nameEn: "Off Sanriku (Oyashio)", latitude: 39.5, longitude: 142.8, region: "Pacific", category: "regional" },
  { id: "oyashio-kushiro", name: "道東・釧路沖", nameEn: "Off Kushiro (Hokkaido)", latitude: 42.6, longitude: 144.8, region: "Pacific", category: "regional" },
  { id: "ishikari-bay", name: "石狩湾", nameEn: "Ishikari Bay", latitude: 43.4, longitude: 141.0, region: "Sea of Japan", category: "regional" },
  { id: "sea-of-japan-central", name: "日本海中部", nameEn: "Central Sea of Japan", latitude: 38.0, longitude: 135.5, region: "Sea of Japan", category: "regional" },
  { id: "wakasa-bay", name: "若狭湾", nameEn: "Wakasa Bay", latitude: 35.85, longitude: 135.3, region: "Sea of Japan", category: "regional" },
  { id: "tsushima-strait", name: "対馬海峡", nameEn: "Tsushima Strait", latitude: 34.1, longitude: 130.2, region: "Sea of Japan", category: "regional" },
  { id: "east-china-sea", name: "東シナ海", nameEn: "East China Sea", latitude: 29.5, longitude: 126.5, region: "East China Sea", category: "regional" },
  { id: "okinawa-offshore", name: "沖縄近海", nameEn: "Off Okinawa", latitude: 25.8, longitude: 127.9, region: "Pacific", category: "regional" },
  { id: "ogasawara-offshore", name: "小笠原近海", nameEn: "Off Ogasawara", latitude: 26.8, longitude: 142.0, region: "Pacific", category: "regional" },
  { id: "nino-3", name: "エルニーニョ監視海域 (NINO.3)", nameEn: "El Niño Monitoring (NINO.3)", latitude: 0.0, longitude: -120.0, region: "Equatorial Pacific", category: "climate" },
  { id: "nino-west", name: "西太平洋熱帯域 (NINO.WEST)", nameEn: "Western Tropical Pacific (NINO.WEST)", latitude: 7.5, longitude: 140.0, region: "Tropical Pacific", category: "climate" },
  { id: "iobw", name: "インド洋熱帯域 (IOBW)", nameEn: "Tropical Indian Ocean (IOBW)", latitude: -2.0, longitude: 80.0, region: "Indian Ocean", category: "climate" },
  { id: "nino-34", name: "エルニーニョ監視海域 (NINO.3.4)", nameEn: "El Niño Monitoring (NINO.3.4)", latitude: 0.0, longitude: -145.0, region: "Equatorial Pacific", category: "climate" },
];

export function cityPresetToLocation(p: CityPreset, order: number, isPrimary = false): ObservationLocation {
  return {
    id: p.id,
    name: p.name,
    nameEn: p.nameEn,
    latitude: p.latitude,
    longitude: p.longitude,
    source: "preset",
    isPrimary,
    order,
  };
}

export function marinePresetToLocation(p: MarinePreset, order: number, isPrimary = false): MarineLocation {
  return {
    id: p.id,
    name: p.name,
    nameEn: p.nameEn,
    latitude: p.latitude,
    longitude: p.longitude,
    region: p.region,
    source: "preset",
    category: p.category ?? "regional",
    isPrimary,
    order,
  };
}
