package analysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

// RankingFactorDescriptor describes an evaluation axis (factor) for municipality safety/livability ranking.
type RankingFactorDescriptor struct {
	ID            string  `json:"id"`            // e.g. "climate", "quake", "disaster", "lore", "volcano"
	DefaultWeight float64 `json:"defaultWeight"` // default importance weight (0..100)
	JapanOnly     bool    `json:"japanOnly,omitempty"`
}

// RankingFactorScore holds the evaluated 0..100 score and human-readable summary metrics for one factor.
type RankingFactorScore struct {
	FactorID   string             `json:"factorId"`
	Score      float64            `json:"score"`      // 0..100 (100 = safest / most comfortable)
	Applicable bool               `json:"applicable"` // false if factor doesn't apply (e.g. Japan-only lore for global city)
	Summary    string             `json:"summary"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Highlights []string           `json:"highlights,omitempty"`
}

// RankingEvalContext holds pre-indexed spatial datasets and climate cache for fast evaluation across 2,000+ municipalities.
type RankingEvalContext struct {
	Config       RankingConfig
	Store        *store.Store
	Now          time.Time
	Since        time.Time
	YearsSpan    float64
	Quakes       []store.QuakeRecord
	Disasters    []store.DisasterRecord
	Lore         []store.DisasterLoreRecord
	Volcanoes    []collector.VolcanoInfo
	Rivers       []store.RiverTimeSeriesPoint
	ClimateMap   map[string]store.MunicipalityClimateStats
	AnchorPoints []climateAnchorPoint

	quakeGrid    map[gridCellKey][]int
	disasterGrid map[gridCellKey][]int
	loreGrid     map[gridCellKey][]int
}

type climateAnchorPoint struct {
	Lat   float64
	Lon   float64
	Stats store.MunicipalityClimateStats
}

type gridCellKey struct {
	latBin int
	lonBin int
}

// RankingFactorProvider abstracts a municipality scoring dimension so new collected data sources
// (e.g. air quality, river flood risk, ground amplification, space weather) can be added seamlessly.
type RankingFactorProvider interface {
	Descriptor() RankingFactorDescriptor
	Evaluate(ctx *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore
}

var (
	rankingFactorMu sync.RWMutex
	rankingFactors  []RankingFactorProvider
)

func init() {
	RegisterRankingFactor(climateFactorProvider{})
	RegisterRankingFactor(quakeFactorProvider{})
	RegisterRankingFactor(disasterFactorProvider{})
	RegisterRankingFactor(loreFactorProvider{})
	RegisterRankingFactor(volcanoFactorProvider{})
}

// RegisterRankingFactor registers a RankingFactorProvider. If a provider with the same ID exists, it is replaced.
func RegisterRankingFactor(p RankingFactorProvider) {
	if p == nil {
		return
	}
	rankingFactorMu.Lock()
	defer rankingFactorMu.Unlock()
	id := p.Descriptor().ID
	for i, existing := range rankingFactors {
		if existing.Descriptor().ID == id {
			rankingFactors[i] = p
			return
		}
	}
	rankingFactors = append(rankingFactors, p)
}

// RegisteredRankingFactors returns all registered RankingFactorProviders in registration order.
func RegisteredRankingFactors() []RankingFactorProvider {
	rankingFactorMu.RLock()
	defer rankingFactorMu.RUnlock()
	out := make([]RankingFactorProvider, len(rankingFactors))
	copy(out, rankingFactors)
	return out
}

// UnregisterRankingFactor removes a factor provider by ID (primarily used in tests for custom factor injection).
func UnregisterRankingFactor(id string) {
	rankingFactorMu.Lock()
	defer rankingFactorMu.Unlock()
	for i, existing := range rankingFactors {
		if existing.Descriptor().ID == id {
			rankingFactors = append(rankingFactors[:i], rankingFactors[i+1:]...)
			return
		}
	}
}

func gridKeyForCoord(lat, lon float64) gridCellKey {
	return gridCellKey{
		latBin: int(math.Floor(lat)),
		lonBin: int(math.Floor(lon)),
	}
}

func (c *RankingEvalContext) buildSpatialGrids() {
	c.quakeGrid = make(map[gridCellKey][]int, 256)
	for i, q := range c.Quakes {
		if q.Latitude == 0 && q.Longitude == 0 {
			continue
		}
		k := gridKeyForCoord(q.Latitude, q.Longitude)
		c.quakeGrid[k] = append(c.quakeGrid[k], i)
	}

	c.disasterGrid = make(map[gridCellKey][]int, 128)
	for i, d := range c.Disasters {
		if d.Latitude == 0 && d.Longitude == 0 {
			continue
		}
		k := gridKeyForCoord(d.Latitude, d.Longitude)
		c.disasterGrid[k] = append(c.disasterGrid[k], i)
	}

	c.loreGrid = make(map[gridCellKey][]int, 128)
	for i, l := range c.Lore {
		if l.Latitude == 0 && l.Longitude == 0 {
			continue
		}
		k := gridKeyForCoord(l.Latitude, l.Longitude)
		c.loreGrid[k] = append(c.loreGrid[k], i)
	}
}

func (c *RankingEvalContext) candidateCells(lat, lon, radiusKm float64) []gridCellKey {
	degLat := int(math.Ceil(radiusKm/111.0)) + 1
	cosLat := math.Cos(lat * math.Pi / 180.0)
	if cosLat < 0.15 {
		cosLat = 0.15
	}
	degLon := int(math.Ceil(radiusKm/(111.0*cosLat))) + 1
	center := gridKeyForCoord(lat, lon)
	cells := make([]gridCellKey, 0, (2*degLat+1)*(2*degLon+1))
	for dLat := -degLat; dLat <= degLat; dLat++ {
		for dLon := -degLon; dLon <= degLon; dLon++ {
			cells = append(cells, gridCellKey{
				latBin: center.latBin + dLat,
				lonBin: center.lonBin + dLon,
			})
		}
	}
	return cells
}

func haversineKmAnalysis(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func clampScore(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 50
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return math.Round(v*10) / 10
}

// ---------------------------------------------------------------------------
// 1. Climate Factor Provider (気候・快適性・少雪・穏やかさ)
// ---------------------------------------------------------------------------

type climateFactorProvider struct{}

func (climateFactorProvider) Descriptor() RankingFactorDescriptor {
	return RankingFactorDescriptor{
		ID:            "climate",
		DefaultWeight: 25,
	}
}

func (climateFactorProvider) Evaluate(ctx *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore {
	stats, ok := ctx.resolveClimateStats(muni)
	if !ok {
		return RankingFactorScore{
			FactorID:   "climate",
			Score:      70,
			Applicable: true,
			Summary:    "No climate data",
		}
	}

	idealTemp := ctx.Config.IdealTempC
	if idealTemp == 0 {
		idealTemp = 16.0
	}

	// Base 100, deduct penalties for deviation from ideal temp, extreme heat, freezing, heavy rain, and snow.
	tempDiff := math.Abs(stats.MeanTempC - idealTemp)
	tempPenalty := math.Min(35.0, tempDiff*2.6)

	heatPenalty := 0.0
	if ctx.Config.PenalizeExtremeHeat {
		heatPenalty = math.Min(25.0, float64(stats.ExtremeHot35)*0.75+float64(stats.HotDays30)*0.18)
	}

	coldPenalty := 0.0
	if ctx.Config.PenalizeExtremeCold {
		coldPenalty = math.Min(25.0, float64(stats.FreezingDays0)*0.22)
	}

	snowPenalty := 0.0
	if ctx.Config.PenalizeHeavySnow {
		snowPenalty = math.Min(28.0, stats.AnnualSnowCm*0.09)
	}

	rainPenalty := math.Min(20.0, float64(stats.HeavyRainDays)*1.8)
	if stats.AnnualPrecipMm > 2200 {
		rainPenalty = math.Min(25.0, rainPenalty+(stats.AnnualPrecipMm-2200)*0.004)
	}

	score := clampScore(100.0 - tempPenalty - heatPenalty - coldPenalty - snowPenalty - rainPenalty)

	var highlights []string
	highlights = append(highlights, fmt.Sprintf("平均気温 %.1f°C (最高 %.1f°C / 最低 %.1f°C)", stats.MeanTempC, stats.MaxTempC, stats.MinTempC))
	if stats.ExtremeHot35 > 0 || stats.HotDays30 > 0 {
		highlights = append(highlights, fmt.Sprintf("猛暑日 %d日/年・真夏日 %d日/年", stats.ExtremeHot35, stats.HotDays30))
	}
	if stats.FreezingDays0 > 0 || stats.AnnualSnowCm > 0 {
		highlights = append(highlights, fmt.Sprintf("冬日 %d日/年・年間降雪 %.0fcm", stats.FreezingDays0, stats.AnnualSnowCm))
	}
	highlights = append(highlights, fmt.Sprintf("年間降水 %.0fmm (大雨日 %d日/年)", stats.AnnualPrecipMm, stats.HeavyRainDays))

	return RankingFactorScore{
		FactorID:   "climate",
		Score:      score,
		Applicable: true,
		Summary:    fmt.Sprintf("%.1f°C / 降雪%.0fcm / 大雨%d日", stats.MeanTempC, stats.AnnualSnowCm, stats.HeavyRainDays),
		Metrics: map[string]float64{
			"meanTempC":      stats.MeanTempC,
			"maxTempC":       stats.MaxTempC,
			"minTempC":       stats.MinTempC,
			"hotDays30":      float64(stats.HotDays30),
			"extremeHot35":   float64(stats.ExtremeHot35),
			"freezingDays0":  float64(stats.FreezingDays0),
			"annualPrecipMm": stats.AnnualPrecipMm,
			"heavyRainDays":  float64(stats.HeavyRainDays),
			"annualSnowCm":   stats.AnnualSnowCm,
		},
		Highlights: highlights,
	}
}

func (ctx *RankingEvalContext) resolveClimateStats(muni store.MunicipalityEntry) (store.MunicipalityClimateStats, bool) {
	if st, ok := ctx.ClimateMap[muni.ID]; ok && st.SampleDays > 0 {
		return st, true
	}

	// IDW interpolation from nearest available anchor points (or latitude-based climate model fallback)
	type distAnchor struct {
		anchor climateAnchorPoint
		distKm float64
	}
	var nearby []distAnchor
	for _, a := range ctx.AnchorPoints {
		d := haversineKmAnalysis(muni.Latitude, muni.Longitude, a.Lat, a.Lon)
		if d < 1.0 {
			cp := a.Stats
			cp.MunicipalityID = muni.ID
			return cp, true
		}
		if d <= 500.0 {
			nearby = append(nearby, distAnchor{anchor: a, distKm: d})
		}
	}

	if len(nearby) > 0 {
		sort.Slice(nearby, func(i, j int) bool {
			return nearby[i].distKm < nearby[j].distKm
		})
		if len(nearby) > 3 {
			nearby = nearby[:3]
		}
		var wSum, meanT, maxT, minT, precip, snow, hot30, hot35, freeze0, heavyRain float64
		for _, n := range nearby {
			w := 1.0 / math.Pow(n.distKm+10.0, 1.5)
			// Apply slight latitude lapse correction (-0.55°C per degree poleward)
			latDiff := math.Abs(muni.Latitude) - math.Abs(n.anchor.Lat)
			tAdj := -latDiff * 0.55
			wSum += w
			meanT += w * (n.anchor.Stats.MeanTempC + tAdj)
			maxT += w * (n.anchor.Stats.MaxTempC + tAdj*0.7)
			minT += w * (n.anchor.Stats.MinTempC + tAdj*1.1)
			precip += w * n.anchor.Stats.AnnualPrecipMm
			snow += w * n.anchor.Stats.AnnualSnowCm
			hot30 += w * float64(n.anchor.Stats.HotDays30)
			hot35 += w * float64(n.anchor.Stats.ExtremeHot35)
			freeze0 += w * float64(n.anchor.Stats.FreezingDays0)
			heavyRain += w * float64(n.anchor.Stats.HeavyRainDays)
		}
		if wSum > 0 {
			return store.MunicipalityClimateStats{
				MunicipalityID: muni.ID,
				RangeKey:       ctx.Config.RangeKey,
				SampleDays:     365,
				MeanTempC:      math.Round((meanT/wSum)*10) / 10,
				MaxTempC:       math.Round((maxT/wSum)*10) / 10,
				MinTempC:       math.Round((minT/wSum)*10) / 10,
				HotDays30:      max(0, int(hot30/wSum+0.5)),
				ExtremeHot35:   max(0, int(hot35/wSum+0.5)),
				FreezingDays0:  max(0, int(freeze0/wSum+0.5)),
				AnnualPrecipMm: math.Round(precip / wSum),
				HeavyRainDays:  max(0, int(heavyRain/wSum+0.5)),
				AnnualSnowCm:   math.Round(snow / wSum),
				Source:         "interpolated",
			}, true
		}
	}

	// Deterministic physical baseline by latitude & Sea-of-Japan / continental coast geometry
	absLat := math.Abs(muni.Latitude)
	estMean := 28.0 - 0.62*absLat
	if muni.Group == "japan" {
		// Calibrate to Japanese archipelago (35.7°N Tokyo ~ 15.8°C, 43.0°N Sapporo ~ 9.2°C, 26.2°N Naha ~ 23.3°C)
		estMean = 15.8 - (muni.Latitude-35.7)*0.82
	}
	estMax := estMean + 17.5
	estMin := estMean - 14.5
	hot30 := max(0, int((estMean-11.0)*6.5))
	hot35 := max(0, int((estMean-14.5)*3.2))
	freeze0 := max(0, int((18.5-estMean)*9.0))
	snowCm := 0.0
	if muni.Group == "japan" {
		// Sea of Japan side & northern Japan snowfall baseline
		isSeaOfJapanSide := (muni.Region == "hokkaido" || muni.Region == "tohoku" || muni.PreIsSeaOfJapan())
		if muni.Latitude >= 35.3 && isSeaOfJapanSide {
			snowCm = math.Max(0, (muni.Latitude-34.5)*38.0)
		} else if muni.Latitude >= 37.0 {
			snowCm = math.Max(0, (muni.Latitude-36.5)*14.0)
		}
	} else if absLat > 42.0 {
		snowCm = (absLat - 40.0) * 12.0
	}
	precipMm := 1450.0
	heavyRain := 3
	if muni.Group == "japan" && muni.Latitude < 34.0 {
		precipMm = 2100.0
		heavyRain = 6
	}
	return store.MunicipalityClimateStats{
		MunicipalityID: muni.ID,
		RangeKey:       ctx.Config.RangeKey,
		SampleDays:     365,
		MeanTempC:      math.Round(estMean*10) / 10,
		MaxTempC:       math.Round(estMax*10) / 10,
		MinTempC:       math.Round(estMin*10) / 10,
		HotDays30:      hot30,
		ExtremeHot35:   hot35,
		FreezingDays0:  freeze0,
		AnnualPrecipMm: precipMm,
		HeavyRainDays:  heavyRain,
		AnnualSnowCm:   math.Round(snowCm),
		Source:         "interpolated",
	}, true
}

// ---------------------------------------------------------------------------
// 2. Earthquake Factor Provider (地震の回数と大きさ)
// ---------------------------------------------------------------------------

type quakeFactorProvider struct{}

func (quakeFactorProvider) Descriptor() RankingFactorDescriptor {
	return RankingFactorDescriptor{
		ID:            "quake",
		DefaultWeight: 35,
	}
}

func (quakeFactorProvider) Evaluate(ctx *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore {
	radiusKm := ctx.Config.QuakeRadiusKm
	if radiusKm <= 0 {
		radiusKm = 75.0
	}
	years := ctx.YearsSpan
	if years < 0.25 {
		years = 1.0
	}

	var count, countM5, countM6 int
	var maxMag float64
	var hazardIndex float64
	var largestTitle string

	cells := ctx.candidateCells(muni.Latitude, muni.Longitude, radiusKm)
	for _, cell := range cells {
		indices := ctx.quakeGrid[cell]
		for _, idx := range indices {
			q := ctx.Quakes[idx]
			d := haversineKmAnalysis(muni.Latitude, muni.Longitude, q.Latitude, q.Longitude)
			if d > radiusKm {
				continue
			}
			count++
			if q.Magnitude >= 5.0 {
				countM5++
			}
			if q.Magnitude >= 6.0 {
				countM6++
			}
			if q.Magnitude > maxMag {
				maxMag = q.Magnitude
				largestTitle = fmt.Sprintf("M%.1f %s (%.0fkm)", q.Magnitude, q.Hypocenter, d)
			}
			// Distance-attenuated seismic impact index
			magWeight := math.Pow(2.2, math.Max(0, q.Magnitude-3.5))
			distAtten := 30.0 / (d + 20.0)
			hazardIndex += magWeight * distAtten
		}
	}

	annualCount := float64(count) / years
	annualHazard := hazardIndex / years

	// Deductions: frequency penalty + max magnitude penalty + large-quake penalty
	freqPenalty := math.Min(45.0, annualHazard*1.8+annualCount*1.2)
	magPenalty := 0.0
	if maxMag >= 4.0 {
		magPenalty = math.Min(40.0, (maxMag-3.8)*9.5)
	}
	majorPenalty := math.Min(20.0, float64(countM6)*7.0+float64(countM5)*2.0)

	score := clampScore(100.0 - freqPenalty - magPenalty - majorPenalty)

	var highlights []string
	highlights = append(highlights, fmt.Sprintf("半径%.0fkm圏内の地震: %d回 (M5以上 %d回 / M6以上 %d回)", radiusKm, count, countM5, countM6))
	if maxMag > 0 {
		highlights = append(highlights, fmt.Sprintf("最大規模: %s", largestTitle))
	} else {
		highlights = append(highlights, "集計期間内に近傍の主要地震なし")
	}

	return RankingFactorScore{
		FactorID:   "quake",
		Score:      score,
		Applicable: true,
		Summary:    fmt.Sprintf("%d回 / 最大M%.1f", count, maxMag),
		Metrics: map[string]float64{
			"quakeCount":    float64(count),
			"quakeCountM5":  float64(countM5),
			"quakeCountM6":  float64(countM6),
			"maxMagnitude":  maxMag,
			"annualCount":   math.Round(annualCount*10) / 10,
			"seismicHazard": math.Round(annualHazard*10) / 10,
		},
		Highlights: highlights,
	}
}

// ---------------------------------------------------------------------------
// 3. Past Disasters & Alerts Factor Provider (過去の災害・気象警報・河川リスク)
// ---------------------------------------------------------------------------

type disasterFactorProvider struct{}

func (disasterFactorProvider) Descriptor() RankingFactorDescriptor {
	return RankingFactorDescriptor{
		ID:            "disaster",
		DefaultWeight: 20,
	}
}

func (disasterFactorProvider) Evaluate(ctx *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore {
	radiusKm := ctx.Config.DisasterRadiusKm
	if radiusKm <= 0 {
		radiusKm = 35.0
	}

	var totalCount, criticalCount, warningCount int
	byCat := make(map[string]int)

	cells := ctx.candidateCells(muni.Latitude, muni.Longitude, radiusKm)
	for _, cell := range cells {
		for _, idx := range ctx.disasterGrid[cell] {
			d := ctx.Disasters[idx]
			dist := haversineKmAnalysis(muni.Latitude, muni.Longitude, d.Latitude, d.Longitude)
			if dist > radiusKm {
				continue
			}
			totalCount++
			byCat[d.Category]++
			switch d.Severity {
			case "critical":
				criticalCount++
			case "warning":
				warningCount++
			}
		}
	}

	// Also check nearby river stations in warning/danger status
	riverRiskCount := 0
	for _, r := range ctx.Rivers {
		if r.Status == "danger" || r.Status == "warning" {
			if haversineKmAnalysis(muni.Latitude, muni.Longitude, r.Latitude, r.Longitude) <= radiusKm {
				riverRiskCount++
			}
		}
	}

	penalty := float64(criticalCount)*12.0 + float64(warningCount)*4.5 + float64(totalCount-criticalCount-warningCount)*1.5 + float64(riverRiskCount)*6.0
	score := clampScore(100.0 - math.Min(90.0, penalty))

	var highlights []string
	highlights = append(highlights, fmt.Sprintf("半径%.0fkm圏内の災害・警報履歴: %d件 (緊急 %d件 / 警戒 %d件)", radiusKm, totalCount, criticalCount, warningCount))
	if len(byCat) > 0 {
		var parts []string
		for k, v := range byCat {
			parts = append(parts, fmt.Sprintf("%s:%d", k, v))
		}
		sort.Strings(parts)
		highlights = append(highlights, "内訳: "+strings.Join(parts, ", "))
	}

	return RankingFactorScore{
		FactorID:   "disaster",
		Score:      score,
		Applicable: true,
		Summary:    fmt.Sprintf("災害・警報 %d件 (緊急%d)", totalCount, criticalCount),
		Metrics: map[string]float64{
			"disasterCount": float64(totalCount),
			"criticalCount": float64(criticalCount),
			"warningCount":  float64(warningCount),
			"riverAlerts":   float64(riverRiskCount),
		},
		Highlights: highlights,
	}
}

// ---------------------------------------------------------------------------
// 4. Natural Disaster Lore Factor Provider (自然災害伝承碑・歴史的災害の少なさ)
// ---------------------------------------------------------------------------

type loreFactorProvider struct{}

func (loreFactorProvider) Descriptor() RankingFactorDescriptor {
	return RankingFactorDescriptor{
		ID:            "lore",
		DefaultWeight: 10,
		JapanOnly:     true,
	}
}

func (loreFactorProvider) Evaluate(ctx *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore {
	if muni.Group != "japan" {
		return RankingFactorScore{
			FactorID:   "lore",
			Score:      100,
			Applicable: false,
			Summary:    "N/A (国外)",
		}
	}

	radiusKm := ctx.Config.LoreRadiusKm
	if radiusKm <= 0 {
		radiusKm = 15.0
	}

	var count, tsunamiCount, floodCount, landslideCount int
	var nearestName string
	nearestDist := math.MaxFloat64

	cells := ctx.candidateCells(muni.Latitude, muni.Longitude, radiusKm)
	for _, cell := range cells {
		for _, idx := range ctx.loreGrid[cell] {
			l := ctx.Lore[idx]
			d := haversineKmAnalysis(muni.Latitude, muni.Longitude, l.Latitude, l.Longitude)
			if d > radiusKm {
				continue
			}
			count++
			switch l.Category {
			case "tsunami":
				tsunamiCount++
			case "flood":
				floodCount++
			case "landslide":
				landslideCount++
			}
			if d < nearestDist {
				nearestDist = d
				nearestName = fmt.Sprintf("%s (%s, %.1fkm)", l.Name, l.DisasterKind, d)
			}
		}
	}

	// Weight tsunami and close-range monuments slightly more heavily
	penalty := float64(count)*4.2 + float64(tsunamiCount)*2.5
	if nearestDist < 3.0 {
		penalty += 8.0
	}
	score := clampScore(100.0 - math.Min(85.0, penalty))

	var highlights []string
	highlights = append(highlights, fmt.Sprintf("半径%.0fkm圏内の自然災害伝承碑: %d基 (津波:%d / 水害:%d / 土砂:%d)", radiusKm, count, tsunamiCount, floodCount, landslideCount))
	if nearestName != "" {
		highlights = append(highlights, "最寄り伝承碑: "+nearestName)
	}

	return RankingFactorScore{
		FactorID:   "lore",
		Score:      score,
		Applicable: true,
		Summary:    fmt.Sprintf("伝承碑 %d基", count),
		Metrics: map[string]float64{
			"loreCount":      float64(count),
			"tsunamiLore":    float64(tsunamiCount),
			"floodLore":      float64(floodCount),
			"landslideLore":  float64(landslideCount),
		},
		Highlights: highlights,
	}
}

// ---------------------------------------------------------------------------
// 5. Volcano Proximity & Alert Level Factor Provider (活火山リスク)
// ---------------------------------------------------------------------------

type volcanoFactorProvider struct{}

func (volcanoFactorProvider) Descriptor() RankingFactorDescriptor {
	return RankingFactorDescriptor{
		ID:            "volcano",
		DefaultWeight: 10,
	}
}

func (volcanoFactorProvider) Evaluate(ctx *RankingEvalContext, muni store.MunicipalityEntry) RankingFactorScore {
	if len(ctx.Volcanoes) == 0 {
		return RankingFactorScore{
			FactorID:   "volcano",
			Score:      95,
			Applicable: true,
			Summary:    "活火山データなし",
		}
	}

	nearestDist := math.MaxFloat64
	var nearestVol collector.VolcanoInfo
	within50km := 0

	for _, v := range ctx.Volcanoes {
		d := haversineKmAnalysis(muni.Latitude, muni.Longitude, v.Latitude, v.Longitude)
		if d <= 50.0 {
			within50km++
		}
		if d < nearestDist {
			nearestDist = d
			nearestVol = v
		}
	}

	penalty := 0.0
	if nearestDist < 60.0 {
		// Inside 60km, closer distance & higher alert level increase penalty
		distFactor := (60.0 - nearestDist) / 60.0
		lvlMult := 1.0 + float64(max(0, nearestVol.AlertLevel-1))*0.65
		penalty = distFactor * 55.0 * lvlMult
	}
	if within50km > 1 {
		penalty += float64(within50km-1) * 5.0
	}

	score := clampScore(100.0 - math.Min(95.0, penalty))

	var highlights []string
	if nearestDist < 5000 {
		highlights = append(highlights, fmt.Sprintf("最寄り活火山: %s (約%.0fkm, 警戒レベル%d)", nearestVol.Name, nearestDist, max(1, nearestVol.AlertLevel)))
	}

	return RankingFactorScore{
		FactorID:   "volcano",
		Score:      score,
		Applicable: true,
		Summary:    fmt.Sprintf("最寄り %.0fkm (%s)", nearestDist, nearestVol.Name),
		Metrics: map[string]float64{
			"nearestVolcanoKm":    math.Round(nearestDist*10) / 10,
			"nearestVolcanoLevel": float64(max(1, nearestVol.AlertLevel)),
			"volcanoesWithin50Km": float64(within50km),
		},
		Highlights: highlights,
	}
}
