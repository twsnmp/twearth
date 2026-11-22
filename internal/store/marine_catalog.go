package store

import (
	"fmt"
	"math"
	"sort"
)

// MarineCatalogEntry describes a built-in marine/climate monitoring coordinate.
type MarineCatalogEntry struct {
	ID       string
	Name     string
	NameEn   string
	Latitude float64
	Longitude float64
	Region   string
	Category string // "nearshore" | "regional" | "climate"
}

var japanMarineCatalog = []MarineCatalogEntry{
	{ID: "tokyo-bay", Name: "東京湾", NameEn: "Tokyo Bay", Latitude: 35.48, Longitude: 139.82, Region: "Pacific", Category: "regional"},
	{ID: "sagami-bay", Name: "相模湾", NameEn: "Sagami Bay", Latitude: 35.15, Longitude: 139.35, Region: "Pacific", Category: "regional"},
	{ID: "suruga-bay", Name: "駿河湾", NameEn: "Suruga Bay", Latitude: 34.90, Longitude: 138.55, Region: "Pacific", Category: "regional"},
	{ID: "enshu-nada", Name: "遠州灘", NameEn: "Enshu-nada", Latitude: 34.35, Longitude: 137.40, Region: "Pacific", Category: "regional"},
	{ID: "kii-channel", Name: "紀伊水道", NameEn: "Kii Channel", Latitude: 33.85, Longitude: 134.90, Region: "Pacific", Category: "regional"},
	{ID: "kuroshio-shionomisaki", Name: "潮岬沖 (黒潮域)", NameEn: "Off Shionomisaki (Kuroshio)", Latitude: 33.25, Longitude: 135.75, Region: "Pacific", Category: "regional"},
	{ID: "hyuga-nada", Name: "日向灘", NameEn: "Hyuga-nada", Latitude: 32.00, Longitude: 131.85, Region: "Pacific", Category: "regional"},
	{ID: "sendai-bay", Name: "仙台湾", NameEn: "Sendai Bay", Latitude: 38.05, Longitude: 141.40, Region: "Pacific", Category: "regional"},
	{ID: "oyashio-sanriku", Name: "三陸沖 (親潮域)", NameEn: "Off Sanriku (Oyashio)", Latitude: 39.50, Longitude: 142.80, Region: "Pacific", Category: "regional"},
	{ID: "oyashio-kushiro", Name: "道東・釧路沖", NameEn: "Off Kushiro (Hokkaido)", Latitude: 42.60, Longitude: 144.80, Region: "Pacific", Category: "regional"},
	{ID: "ishikari-bay", Name: "石狩湾", NameEn: "Ishikari Bay", Latitude: 43.40, Longitude: 141.00, Region: "Sea of Japan", Category: "regional"},
	{ID: "sea-of-japan-central", Name: "日本海中部", NameEn: "Central Sea of Japan", Latitude: 38.00, Longitude: 135.50, Region: "Sea of Japan", Category: "regional"},
	{ID: "wakasa-bay", Name: "若狭湾", NameEn: "Wakasa Bay", Latitude: 35.85, Longitude: 135.30, Region: "Sea of Japan", Category: "regional"},
	{ID: "tsushima-strait", Name: "対馬海峡", NameEn: "Tsushima Strait", Latitude: 34.10, Longitude: 130.20, Region: "Sea of Japan", Category: "regional"},
	{ID: "east-china-sea", Name: "東シナ海", NameEn: "East China Sea", Latitude: 29.50, Longitude: 126.50, Region: "East China Sea", Category: "regional"},
	{ID: "okinawa-offshore", Name: "沖縄近海", NameEn: "Off Okinawa", Latitude: 25.80, Longitude: 127.90, Region: "Pacific", Category: "regional"},
	{ID: "ogasawara-offshore", Name: "小笠原近海", NameEn: "Off Ogasawara", Latitude: 26.80, Longitude: 142.00, Region: "Pacific", Category: "regional"},
}

var globalMarineCatalog = []MarineCatalogEntry{
	{ID: "new-york-bight", Name: "ニューヨーク沖 (大西洋)", NameEn: "Off New York (Atlantic)", Latitude: 40.20, Longitude: -73.30, Region: "Atlantic", Category: "regional"},
	{ID: "english-channel", Name: "英仏海峡", NameEn: "English Channel", Latitude: 50.30, Longitude: -1.50, Region: "Atlantic", Category: "regional"},
	{ID: "north-sea", Name: "北海", NameEn: "North Sea", Latitude: 53.50, Longitude: 3.00, Region: "Atlantic", Category: "regional"},
	{ID: "mediterranean-west", Name: "地中海西部", NameEn: "Western Mediterranean", Latitude: 40.50, Longitude: 4.50, Region: "Mediterranean", Category: "regional"},
	{ID: "us-west-california", Name: "カリフォルニア沖", NameEn: "Off California", Latitude: 36.50, Longitude: -122.50, Region: "Pacific", Category: "regional"},
	{ID: "hawaii-offshore", Name: "ハワイ近海", NameEn: "Off Hawaii", Latitude: 21.00, Longitude: -158.50, Region: "Pacific", Category: "regional"},
	{ID: "singapore-scs", Name: "南シナ海南部 (シンガポール沖)", NameEn: "Southern South China Sea", Latitude: 2.50, Longitude: 105.00, Region: "South China Sea", Category: "regional"},
	{ID: "tasman-sea-sydney", Name: "タスマン海 (シドニー沖)", NameEn: "Tasman Sea (Off Sydney)", Latitude: -33.90, Longitude: 151.80, Region: "Tasman Sea", Category: "regional"},
}

var climateMarineCatalog = []MarineCatalogEntry{
	{ID: "nino-3", Name: "エルニーニョ監視海域 (NINO.3)", NameEn: "El Niño Monitoring (NINO.3)", Latitude: 0.00, Longitude: -120.00, Region: "Equatorial Pacific", Category: "climate"},
	{ID: "nino-west", Name: "西太平洋熱帯域 (NINO.WEST)", NameEn: "Western Tropical Pacific (NINO.WEST)", Latitude: 7.50, Longitude: 140.00, Region: "Tropical Pacific", Category: "climate"},
	{ID: "iobw", Name: "インド洋熱帯域 (IOBW)", NameEn: "Tropical Indian Ocean (IOBW)", Latitude: -2.00, Longitude: 80.00, Region: "Indian Ocean", Category: "climate"},
	{ID: "nino-34", Name: "エルニーニョ監視海域 (NINO.3.4)", NameEn: "El Niño Monitoring (NINO.3.4)", Latitude: 0.00, Longitude: -145.00, Region: "Equatorial Pacific", Category: "climate"},
}

// Japan regional balance representatives (Kuroshio, Oyashio, Sea of Japan, East China Sea, Okinawa).
var japanRegionalPriorityIDs = []string{
	"kuroshio-shionomisaki",
	"oyashio-sanriku",
	"sea-of-japan-central",
	"east-china-sea",
	"okinawa-offshore",
}

// Default climate monitoring IDs (Option A: JMA NINO.3 + NINO.WEST + IOBW).
var defaultClimateIDs = []string{
	"nino-3",
	"nino-west",
	"iobw",
}

func isJapanCoord(lat, lon float64) bool {
	return lat >= 20.0 && lat <= 46.5 && lon >= 122.0 && lon <= 154.0
}

func haversineKmStore(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

func entryToMarineLocation(e MarineCatalogEntry, source, category string, isPrimary bool, order int) MarineLocation {
	if category == "" {
		category = e.Category
	}
	return MarineLocation{
		ID:        e.ID,
		Name:      e.Name,
		NameEn:    e.NameEn,
		Latitude:  e.Latitude,
		Longitude: e.Longitude,
		Region:    e.Region,
		Source:    source,
		Category:  category,
		IsPrimary: isPrimary,
		Order:     order,
	}
}

func findCatalogEntry(id string) (MarineCatalogEntry, bool) {
	for _, list := range [][]MarineCatalogEntry{japanMarineCatalog, globalMarineCatalog, climateMarineCatalog} {
		for _, e := range list {
			if e.ID == id {
				return e, true
			}
		}
	}
	return MarineCatalogEntry{}, false
}

func marineAutoKey(primary ObservationLocation, includeClimate bool) string {
	return fmt.Sprintf("%s:%.3f,%.3f:%t", primary.ID, primary.Latitude, primary.Longitude, includeClimate)
}

// RecommendMarineLocations selects nearshore, regional, and optional climate-teleconnection sea areas
// tailored to the primary land ObservationLocation.
func RecommendMarineLocations(primary ObservationLocation, includeClimate bool) []MarineLocation {
	type scoredEntry struct {
		entry MarineCatalogEntry
		dist  float64
	}

	var out []MarineLocation
	added := make(map[string]bool)
	add := func(e MarineCatalogEntry, category string, isPrimary bool) {
		if added[e.ID] {
			return
		}
		added[e.ID] = true
		out = append(out, entryToMarineLocation(e, "auto", category, isPrimary, len(out)))
	}

	if isJapanCoord(primary.Latitude, primary.Longitude) {
		scored := make([]scoredEntry, 0, len(japanMarineCatalog))
		for _, e := range japanMarineCatalog {
			scored = append(scored, scoredEntry{
				entry: e,
				dist:  haversineKmStore(primary.Latitude, primary.Longitude, e.Latitude, e.Longitude),
			})
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].dist < scored[j].dist
		})

		// 1. Closest 2 nearshore points (#1 becomes primary).
		for i := 0; i < len(scored) && i < 2; i++ {
			add(scored[i].entry, "nearshore", i == 0)
		}

		// 2. Balance across major Japanese sea currents/basins (up to 4 additional regional points).
		regionalAdded := 0
		for _, id := range japanRegionalPriorityIDs {
			if regionalAdded >= 4 {
				break
			}
			if added[id] {
				continue
			}
			if e, ok := findCatalogEntry(id); ok {
				add(e, "regional", false)
				regionalAdded++
			}
		}
	} else {
		allOcean := make([]MarineCatalogEntry, 0, len(globalMarineCatalog)+len(japanMarineCatalog))
		allOcean = append(allOcean, globalMarineCatalog...)
		allOcean = append(allOcean, japanMarineCatalog...)

		scored := make([]scoredEntry, 0, len(allOcean))
		for _, e := range allOcean {
			scored = append(scored, scoredEntry{
				entry: e,
				dist:  haversineKmStore(primary.Latitude, primary.Longitude, e.Latitude, e.Longitude),
			})
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].dist < scored[j].dist
		})

		for i := 0; i < len(scored) && i < 3; i++ {
			if i > 0 && scored[i].dist > 3500 {
				break
			}
			cat := "regional"
			if i == 0 {
				cat = "nearshore"
			}
			add(scored[i].entry, cat, i == 0)
		}
	}

	// 3. El Niño / Climate teleconnection monitoring regions (affecting Japan & global weather).
	if includeClimate {
		for _, id := range defaultClimateIDs {
			if e, ok := findCatalogEntry(id); ok {
				add(e, "climate", false)
			}
		}
	}

	return out
}

// MergeAutoMarineLocations replaces "auto"-sourced marine entries with newly recommended ones
// for primary while preserving any manually added ("preset" / "custom") marine locations.
func MergeAutoMarineLocations(existing []MarineLocation, primary ObservationLocation, includeClimate bool) []MarineLocation {
	recs := RecommendMarineLocations(primary, includeClimate)

	var manual []MarineLocation
	manualPrimary := false
	for _, loc := range existing {
		if loc.Source == "preset" || loc.Source == "custom" {
			if loc.IsPrimary {
				manualPrimary = true
			}
			manual = append(manual, loc)
		}
	}

	usedIdx := make(map[string]int, len(recs)+len(manual))
	out := make([]MarineLocation, 0, len(recs)+len(manual))

	for _, r := range recs {
		if manualPrimary {
			r.IsPrimary = false
		}
		usedIdx[r.ID] = len(out)
		r.Order = len(out)
		out = append(out, r)
	}
	for _, m := range manual {
		if idx, exists := usedIdx[m.ID]; exists {
			if m.IsPrimary {
				out[idx].IsPrimary = true
			}
			out[idx].Source = m.Source
			continue
		}
		usedIdx[m.ID] = len(out)
		m.Order = len(out)
		out = append(out, m)
	}
	return out
}
