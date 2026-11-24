package analysis

import (
	"fmt"
	"math"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

// BuildCatalog returns available weather/marine locations and metrics from config.
func BuildCatalog(cfg store.Config) SeriesCatalog {
	locs := make([]CatalogLocation, 0, len(cfg.Locations)+len(cfg.MarineLocations))
	for _, l := range cfg.Locations {
		locs = append(locs, CatalogLocation{
			ID:     l.ID,
			Kind:   "weather",
			Name:   l.Name,
			NameEn: l.NameEn,
		})
	}
	for _, m := range cfg.MarineLocations {
		locs = append(locs, CatalogLocation{
			ID:       m.ID,
			Kind:     "marine",
			Name:     m.Name,
			NameEn:   m.NameEn,
			Category: m.Category,
			Region:   m.Region,
		})
	}

	metrics := []CatalogMetric{
		{ID: "temperature2m", Kind: "weather", Unit: "°C"},
		{ID: "pressureMsl", Kind: "weather", Unit: "hPa"},
		{ID: "precipitation", Kind: "weather", Unit: "mm"},
		{ID: "snowfall", Kind: "weather", Unit: "cm"},
		{ID: "windSpeed10m", Kind: "weather", Unit: "km/h"},
		{ID: "cape", Kind: "weather", Unit: "J/kg"},
		{ID: "seaSurfaceTemperature", Kind: "marine", Unit: "°C"},
		{ID: "waveHeight", Kind: "marine", Unit: "m"},
		{ID: "oceanCurrentVelocity", Kind: "marine", Unit: "km/h"},
	}

	return SeriesCatalog{
		Locations: locs,
		Metrics:   metrics,
	}
}

func resolveGranularity(rangeKey, granularity string) string {
	switch granularity {
	case "hourly", "daily", "monthly":
		return granularity
	}
	switch rangeKey {
	case "30d", "90d", "1y":
		return "daily"
	default:
		return "monthly"
	}
}

func rangeToFromTS(rangeKey string, now time.Time) string {
	switch rangeKey {
	case "30d":
		return now.AddDate(0, 0, -30).Format("200601021504")
	case "90d":
		return now.AddDate(0, 0, -90).Format("200601021504")
	case "1y":
		return now.AddDate(-1, 0, 0).Format("200601021504")
	case "3y":
		return now.AddDate(-3, 0, 0).Format("200601021504")
	case "5y":
		return now.AddDate(-5, 0, 0).Format("200601021504")
	case "10y":
		return now.AddDate(-10, 0, 0).Format("200601021504")
	case "30y":
		return now.AddDate(-30, 0, 0).Format("200601021504")
	default: // "all" or empty
		return ""
	}
}

// LoadAlignedSeries fetches X and Y from the store, aligns by timestamp, and applies preprocessing.
func LoadAlignedSeries(s *store.Store, req CorrelationRequest) ([]TimePairPoint, string, []string, error) {
	if s == nil {
		return nil, "", nil, fmt.Errorf("store is not initialized")
	}
	if req.SeriesX.LocationID == "" || req.SeriesY.LocationID == "" {
		return nil, "", nil, fmt.Errorf("both seriesX and seriesY locationId are required")
	}

	granularity := resolveGranularity(req.RangeKey, req.Granularity)
	now := time.Now().UTC()
	fromTS := rangeToFromTS(req.RangeKey, now)
	toTS := now.Format("200601021504")

	resX, err := s.QueryHistory(store.HistoryQuery{
		Kind:            req.SeriesX.Kind,
		LocationID:      req.SeriesX.LocationID,
		FromTS:          fromTS,
		ToTS:            toTS,
		Metric:          req.SeriesX.Metric,
		Granularity:     granularity,
		MovingAvgWindow: 1,
	})
	if err != nil {
		return nil, granularity, nil, fmt.Errorf("query seriesX: %w", err)
	}

	resY, err := s.QueryHistory(store.HistoryQuery{
		Kind:            req.SeriesY.Kind,
		LocationID:      req.SeriesY.LocationID,
		FromTS:          fromTS,
		ToTS:            toTS,
		Metric:          req.SeriesY.Metric,
		Granularity:     granularity,
		MovingAvgWindow: 1,
	})
	if err != nil {
		return nil, granularity, nil, fmt.Errorf("query seriesY: %w", err)
	}

	if len(resX.Points) == 0 {
		return nil, granularity, nil, fmt.Errorf("no data points found for series X (%s:%s)", req.SeriesX.LocationID, req.SeriesX.Metric)
	}
	if len(resY.Points) == 0 {
		return nil, granularity, nil, fmt.Errorf("no data points found for series Y (%s:%s)", req.SeriesY.LocationID, req.SeriesY.Metric)
	}

	yMap := make(map[string]store.HistoryDataPoint, len(resY.Points))
	for _, pt := range resY.Points {
		yMap[pt.Timestamp] = pt
	}

	pairs := make([]TimePairPoint, 0, min(len(resX.Points), len(resY.Points)))
	for _, px := range resX.Points {
		py, ok := yMap[px.Timestamp]
		if !ok {
			continue
		}
		if math.IsNaN(px.Value) || math.IsInf(px.Value, 0) || math.IsNaN(py.Value) || math.IsInf(py.Value, 0) {
			continue
		}
		label := py.Label
		if label == "" {
			label = px.Label
		}
		pairs = append(pairs, TimePairPoint{
			Timestamp: px.Timestamp,
			Label:     label,
			X:         px.Value,
			Y:         py.Value,
		})
	}

	if len(pairs) < 3 {
		return nil, granularity, nil, fmt.Errorf("insufficient overlapping points (%d) between X and Y", len(pairs))
	}

	var warnings []string
	pairs, w := preprocessPairs(pairs, granularity, req.Preprocess, req.Algorithm != "cross_correlation")
	warnings = append(warnings, w...)

	if len(pairs) < 3 {
		return nil, granularity, warnings, fmt.Errorf("insufficient points (%d) after preprocessing", len(pairs))
	}
	return pairs, granularity, warnings, nil
}

func preprocessPairs(pairs []TimePairPoint, granularity string, opts PreprocessOptions, applyLag bool) ([]TimePairPoint, []string) {
	var warnings []string
	out := make([]TimePairPoint, len(pairs))
	copy(out, pairs)

	if opts.Deseasonalize {
		applied, warn := deseasonalizePairs(out, granularity)
		out = applied
		if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	if opts.MovingAvgWindow > 1 {
		out = movingAvgPairs(out, opts.MovingAvgWindow)
	}

	if opts.Difference && len(out) > 1 {
		diffed := make([]TimePairPoint, 0, len(out)-1)
		for i := 1; i < len(out); i++ {
			diffed = append(diffed, TimePairPoint{
				Timestamp: out[i].Timestamp,
				Label:     out[i].Label,
				X:         out[i].X - out[i-1].X,
				Y:         out[i].Y - out[i-1].Y,
			})
		}
		out = diffed
	}

	if applyLag && opts.Lag != 0 {
		out = shiftLagPairs(out, opts.Lag)
	}

	if opts.Standardize && len(out) > 1 {
		out = standardizePairs(out)
	}

	return out, warnings
}

func seasonBucketKey(ts, granularity string) string {
	if len(ts) < 10 {
		return ts
	}
	switch granularity {
	case "hourly":
		return ts[8:10] // hour of day 00..23
	default:
		return ts[4:6] // month of year 01..12
	}
}

func deseasonalizePairs(pairs []TimePairPoint, granularity string) ([]TimePairPoint, string) {
	type acc struct {
		sumX  float64
		sumY  float64
		count int
	}
	groups := make(map[string]*acc)
	for _, p := range pairs {
		k := seasonBucketKey(p.Timestamp, granularity)
		g := groups[k]
		if g == nil {
			g = &acc{}
			groups[k] = g
		}
		g.sumX += p.X
		g.sumY += p.Y
		g.count++
	}

	multiSampleBuckets := 0
	for _, g := range groups {
		if g.count >= 2 {
			multiSampleBuckets++
		}
	}
	if multiSampleBuckets == 0 {
		return pairs, "Deseasonalize skipped: need >=2 cycles per season bucket (e.g. >=2 years for monthly)"
	}

	for i := range pairs {
		k := seasonBucketKey(pairs[i].Timestamp, granularity)
		g := groups[k]
		if g != nil && g.count >= 2 {
			pairs[i].X -= g.sumX / float64(g.count)
			pairs[i].Y -= g.sumY / float64(g.count)
		}
	}
	return pairs, ""
}

func movingAvgPairs(pairs []TimePairPoint, window int) []TimePairPoint {
	if window <= 1 || len(pairs) == 0 {
		return pairs
	}
	out := make([]TimePairPoint, len(pairs))
	var sumX, sumY float64
	for i := range pairs {
		sumX += pairs[i].X
		sumY += pairs[i].Y
		if i >= window {
			sumX -= pairs[i-window].X
			sumY -= pairs[i-window].Y
		}
		denom := float64(min(i+1, window))
		out[i] = TimePairPoint{
			Timestamp: pairs[i].Timestamp,
			Label:     pairs[i].Label,
			X:         sumX / denom,
			Y:         sumY / denom,
		}
	}
	return out
}

func shiftLagPairs(pairs []TimePairPoint, lag int) []TimePairPoint {
	if lag == 0 || len(pairs) == 0 {
		return pairs
	}
	absLag := lag
	if absLag < 0 {
		absLag = -absLag
	}
	if absLag >= len(pairs)-2 {
		return nil
	}
	out := make([]TimePairPoint, 0, len(pairs)-absLag)
	if lag > 0 {
		// Positive lag: X[i - lag] predicts Y[i]
		for i := lag; i < len(pairs); i++ {
			out = append(out, TimePairPoint{
				Timestamp: pairs[i].Timestamp,
				Label:     pairs[i].Label,
				X:         pairs[i-lag].X,
				Y:         pairs[i].Y,
			})
		}
	} else {
		// Negative lag: X[i + absLag] paired with Y[i]
		for i := 0; i < len(pairs)-absLag; i++ {
			out = append(out, TimePairPoint{
				Timestamp: pairs[i].Timestamp,
				Label:     pairs[i].Label,
				X:         pairs[i+absLag].X,
				Y:         pairs[i].Y,
			})
		}
	}
	return out
}

func standardizePairs(pairs []TimePairPoint) []TimePairPoint {
	n := float64(len(pairs))
	var sumX, sumY float64
	for _, p := range pairs {
		sumX += p.X
		sumY += p.Y
	}
	meanX, meanY := sumX/n, sumY/n

	var varX, varY float64
	for _, p := range pairs {
		dx := p.X - meanX
		dy := p.Y - meanY
		varX += dx * dx
		varY += dy * dy
	}
	stdX := math.Sqrt(varX / n)
	stdY := math.Sqrt(varY / n)

	out := make([]TimePairPoint, len(pairs))
	for i, p := range pairs {
		x := p.X - meanX
		y := p.Y - meanY
		if stdX > 1e-12 {
			x /= stdX
		}
		if stdY > 1e-12 {
			y /= stdY
		}
		out[i] = TimePairPoint{
			Timestamp: p.Timestamp,
			Label:     p.Label,
			X:         x,
			Y:         y,
		}
	}
	return out
}
