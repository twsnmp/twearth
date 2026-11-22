package collector

import (
	"math"
	"strings"

	"github.com/twsnmp/twearth/internal/store"
)

// AlertSeverityMeta carries spatial and hazard context for computing effective alert severity.
type AlertSeverityMeta struct {
	Source    string
	Category  string
	Code      int
	Magnitude float64
	MaxScale  string
	Tsunami   string
	Latitude  float64
	Longitude float64
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "critical"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}

func severityToRank(s string) int {
	switch normalizeSeverity(s) {
	case "critical":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}

func rankToSeverity(r int) string {
	switch {
	case r >= 3:
		return "critical"
	case r == 2:
		return "warning"
	default:
		return "info"
	}
}

// DistanceToReferenceLocations calculates distance in km to the nearest configured location
// (or primary location when cfg.AlertDistanceMode == "primary").
func DistanceToReferenceLocations(lat, lon float64, cfg store.Config) (distKm float64, refName string, hasCoord bool) {
	if (lat == 0 && lon == 0) || math.IsNaN(lat) || math.IsNaN(lon) {
		return 0, "", false
	}

	var refs []store.ObservationLocation
	if cfg.AlertDistanceMode == "primary" {
		refs = []store.ObservationLocation{cfg.PrimaryLocation()}
	} else if len(cfg.Locations) > 0 {
		refs = cfg.Locations
	} else {
		refs = []store.ObservationLocation{cfg.PrimaryLocation()}
	}

	best := math.MaxFloat64
	bestName := ""
	for _, loc := range refs {
		if loc.Latitude == 0 && loc.Longitude == 0 {
			continue
		}
		d := HaversineKm(loc.Latitude, loc.Longitude, lat, lon)
		if d < best {
			best = d
			bestName = loc.Name
		}
	}
	if best == math.MaxFloat64 {
		return 0, "", false
	}
	return best, bestName, true
}

// AdjustSeverityByDistance downgrades alert severity based on distance from reference locations,
// preserving a "warning" floor for domestic/regional major earthquakes and active tsunami warnings.
func AdjustSeverityByDistance(baseSeverity string, meta AlertSeverityMeta, cfg store.Config) (effective string, distKm float64, refName string) {
	base := normalizeSeverity(baseSeverity)
	baseRank := severityToRank(base)

	// Non-geographic global events (e.g. Space Weather) keep their base severity.
	if strings.EqualFold(meta.Category, "space") || strings.EqualFold(meta.Source, "space") {
		return base, 0, ""
	}

	distKm, refName, hasCoord := DistanceToReferenceLocations(meta.Latitude, meta.Longitude, cfg)
	if !hasCoord {
		if meta.Code == 552 {
			if strings.EqualFold(meta.Tsunami, "Cancelled") {
				return "info", 0, ""
			}
			if strings.EqualFold(meta.Tsunami, "Watch") && baseRank > 2 {
				return "warning", 0, ""
			}
		}
		return base, 0, ""
	}

	nearKm := cfg.AlertNearRadiusKm
	if nearKm <= 0 {
		nearKm = 100.0
	}
	mediumKm := cfg.AlertMediumRadiusKm
	if mediumKm <= 0 {
		mediumKm = 300.0
	}
	if mediumKm < nearKm {
		mediumKm = nearKm
	}

	rank := baseRank
	if distKm > mediumKm {
		rank = 1
	} else if distKm > nearKm {
		if rank > 1 {
			rank--
		}
	}

	// Domestic/regional major earthquake and tsunami warning floor ("warning" minimum).
	if baseRank >= 2 && rank < 2 {
		isActiveTsunamiWarning := meta.Code == 552 &&
			!strings.EqualFold(meta.Tsunami, "Cancelled") &&
			!strings.EqualFold(meta.Tsunami, "Watch") &&
			!strings.EqualFold(meta.Tsunami, "None")
		if isActiveTsunamiWarning && (strings.EqualFold(meta.Source, "p2pquake") || distKm <= 1500.0) {
			rank = 2
		}

		isMajorDomesticQuake := IntensityRank(meta.MaxScale) >= 55 ||
			(meta.Magnitude >= 7.0 && (strings.EqualFold(meta.Source, "p2pquake") || distKm <= 1500.0))
		if isMajorDomesticQuake {
			rank = 2
		}
	}

	return rankToSeverity(rank), distKm, refName
}

// AdjustQuakeSeverityByDistance applies distance attenuation to an earthquake's intrinsic severity.
func AdjustQuakeSeverityByDistance(baseSeverity string, ev QuakeEvent, cfg store.Config) (effective string, distKm float64, refName string) {
	return AdjustSeverityByDistance(baseSeverity, AlertSeverityMeta{
		Source:    ev.Source,
		Category:  "quake",
		Code:      ev.Code,
		Magnitude: ev.Magnitude,
		MaxScale:  ev.MaxScale,
		Tsunami:   ev.Tsunami,
		Latitude:  ev.Latitude,
		Longitude: ev.Longitude,
	}, cfg)
}

func classifyQuakeSeverity(ev QuakeEvent, minIntensity string) string {
	if ev.Code == 552 {
		if strings.EqualFold(ev.Tsunami, "Cancelled") {
			return "info"
		}
		if strings.EqualFold(ev.Tsunami, "Watch") {
			return "warning"
		}
		return "critical"
	}
	if ev.Code == 556 {
		return "critical"
	}
	rank := IntensityRank(ev.MaxScale)
	minRank := IntensityRank(minIntensity)
	if minRank <= 0 {
		minRank = 30
	}
	if rank >= 45 || ev.Magnitude >= 6.0 {
		return "critical"
	}
	if (rank > 0 && rank >= minRank) || (rank == 0 && ev.Magnitude >= 5.0) {
		return "warning"
	}
	return "info"
}

func classifyVolcanoSeverity(alertLevel int) string {
	if alertLevel >= 4 {
		return "critical"
	}
	if alertLevel >= 2 {
		return "warning"
	}
	return "info"
}

func classifyPressureSeverity(delta3h, threshold float64) string {
	if delta3h <= -threshold*2 || delta3h <= -8.0 {
		return "critical"
	}
	return "warning"
}

func classifyPrecipSeverity(val, threshold float64) string {
	if threshold > 0 && val >= threshold*2 {
		return "critical"
	}
	return "warning"
}

func classifyLightningSeverity(nearbyCount int) string {
	if nearbyCount >= 10 {
		return "critical"
	}
	return "warning"
}

func classifySpaceSeverity(kp float64, flareClass string) string {
	if kp >= 7.0 || flareClass == "X" {
		return "critical"
	}
	return "warning"
}
