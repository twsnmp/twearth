package collector

import (
	"math"
	"testing"
)

func TestBuildSynopticDataGeneratesIsobarsAndCenters(t *testing.T) {
	// Construct a 5x5 grid with a clear Low center at (35, 135) and High ridge at corners.
	var pts []WindGridPoint
	for lat := 25.0; lat <= 45.0; lat += 5.0 {
		for lon := 125.0; lon <= 145.0; lon += 5.0 {
			dist := math.Hypot(lat-35.0, lon-135.0)
			pressure := 996.0 + dist*2.0 // 996 hPa at center, ~1024 hPa at corners
			pts = append(pts, WindGridPoint{
				Latitude:    lat,
				Longitude:   lon,
				PressureMSL: pressure,
			})
		}
	}

	syn := BuildSynopticData(pts)
	if len(syn.Isobars) == 0 {
		t.Fatalf("expected isobar contour lines, got 0")
	}
	foundLow := false
	for _, c := range syn.Centers {
		if c.Kind == "L" && c.Latitude == 35.0 && c.Longitude == 135.0 {
			foundLow = true
		}
	}
	if !foundLow {
		t.Fatalf("expected Low center at (35, 135), got %+v", syn.Centers)
	}
}
