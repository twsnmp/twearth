//go:build !android && !ios

package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/twsnmp/twearth/internal/analysis"
)

func strArrProp(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": desc,
		"items":       map[string]any{"type": "string"},
	}
}

func boolArrProp(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": desc,
		"items":       map[string]any{"type": "boolean"},
	}
}

func numMapProp(desc string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          desc,
		"additionalProperties": map[string]any{"type": "number"},
	}
}

func correlationToolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "get_correlation_catalog",
			Description: "List available time-series kinds, registered locations, metrics, and supported statistical/ML algorithms for correlation analysis.",
			InputSchema: objSchema(nil, nil),
		},
		{
			Name:        "analyze_correlation",
			Description: "Run statistical (pearson, spearman, cross_correlation) or tensai ML (tensai_mlp, tensai_lstm, tensai_knn) correlation analysis between two time series.",
			InputSchema: objSchema(map[string]any{
				"x_kind":            strProp("Series X kind: weather or marine."),
				"x_location_id":     strProp("Series X location ID."),
				"x_metric":          strProp("Series X metric ID (e.g. temperature2m, pressureMsl, seaSurfaceTemperature)."),
				"y_kind":            strProp("Series Y kind: weather or marine."),
				"y_location_id":     strProp("Series Y location ID."),
				"y_metric":          strProp("Series Y metric ID."),
				"range_key":         strProp("Time range: 30d, 90d, 1y, 3y, 5y, 10y, 30y, or all (default 1y)."),
				"granularity":       strProp("Time granularity: auto, hourly, daily, or monthly (default auto)."),
				"algorithm":         strProp("Algorithm: pearson, spearman, cross_correlation, tensai_mlp, tensai_lstm, or tensai_knn (default pearson)."),
				"deseasonalize":     boolProp("Remove seasonal cycle before analysis (recommended to avoid spurious seasonal correlations)."),
				"moving_avg_window": intProp("Moving average smoothing window size (0 = disabled)."),
				"difference":        boolProp("Use first-order differences (step-to-step changes) before analysis."),
				"standardize":       boolProp("Z-score standardize series before analysis."),
				"lag":               intProp("Fixed lag shift: positive lag pairs X[t-lag] with Y[t]."),
				"max_lag":           intProp("Maximum lag sweep for cross_correlation (default 12)."),
				"lag_window":        intProp("Past window size for tensai ML models."),
				"hidden_size":       intProp("Hidden units for tensai_mlp / tensai_lstm."),
				"epochs":            intProp("Training epochs for tensai_mlp / tensai_lstm."),
				"learning_rate":     numProp("Learning rate for tensai_mlp / tensai_lstm."),
				"activation":        strProp("Activation function for tensai_mlp: tanh, relu, or leaky_relu."),
				"train_ratio":       numProp("Chronological train/test split ratio (e.g. 0.8)."),
				"k":                 intProp("Neighbors count K for tensai_knn."),
				"classes":           intProp("Quantile classes (2 or 3) for tensai_knn."),
				"include_series":    boolProp("If true, include downsampled aligned timeSeries points (default false to save tokens)."),
				"max_series_points": intProp("Maximum number of timeSeries points to return when include_series is true (default 120)."),
			}, []string{"x_kind", "x_location_id", "x_metric", "y_kind", "y_location_id", "y_metric"}),
		},
		{
			Name:        "evaluate_correlation_robustness",
			Description: "Evaluate whether a correlation between two series is robust or likely spurious (e.g. seasonal artifact) by comparing raw, deseasonalized, differenced, and combined preprocessing modes.",
			InputSchema: objSchema(map[string]any{
				"x_kind":        strProp("Series X kind: weather or marine."),
				"x_location_id": strProp("Series X location ID."),
				"x_metric":      strProp("Series X metric ID."),
				"y_kind":        strProp("Series Y kind: weather or marine."),
				"y_location_id": strProp("Series Y location ID."),
				"y_metric":      strProp("Series Y metric ID."),
				"range_key":     strProp("Time range: 30d, 90d, 1y, 3y, 5y, 10y, 30y, or all (default 3y)."),
				"granularity":   strProp("Time granularity: auto, hourly, daily, or monthly (default monthly)."),
				"algorithm":     strProp("Statistical algorithm to compare across modes: pearson, spearman, or cross_correlation (default cross_correlation)."),
				"max_lag":       intProp("Maximum lag sweep when algorithm is cross_correlation (default 12)."),
			}, []string{"x_kind", "x_location_id", "x_metric", "y_kind", "y_location_id", "y_metric"}),
		},
		{
			Name:        "manage_correlation_discovery",
			Description: "Control or inspect the background correlation discovery job (status, start, pause, resume, clear_cache, delete_cache, rerun).",
			InputSchema: objSchema(map[string]any{
				"action":                strProp("Action: status, start, pause, resume, clear_cache, delete_cache, or rerun."),
				"kinds":                 strArrProp("Data kinds to include when starting discovery (e.g. [\"weather\", \"marine\"])."),
				"location_ids":          strArrProp("Optional location IDs to restrict the search space."),
				"metrics":               strArrProp("Optional metric IDs to restrict the search space."),
				"range_key":             strProp("Time range for discovery (default 5y)."),
				"granularity":           strProp("Granularity for discovery (default monthly)."),
				"algorithms":            strArrProp("Algorithms to evaluate (default [\"cross_correlation\"])."),
				"deseasonalize_modes":   boolArrProp("Deseasonalize modes to sweep (e.g. [true])."),
				"difference":            boolProp("Apply first-order differencing."),
				"moving_avg_window":     intProp("Moving average smoothing window."),
				"max_lag":               intProp("Max lag for cross_correlation (default 12)."),
				"min_samples":           intProp("Minimum aligned samples required (default 12)."),
				"same_location_allowed": boolProp("Allow pairing metrics within the same location."),
				"cross_kind_only":       boolProp("Only pair series across different kinds (e.g. weather vs marine)."),
				"force_rerun":           boolProp("Recompute even if cached."),
				"keys":                  strArrProp("Cache entry keys for delete_cache or rerun actions."),
			}, []string{"action"}),
		},
		{
			Name:        "list_correlation_discoveries",
			Description: "List or filter cached correlation discovery results sorted by score descending, or fetch a specific entry by key.",
			InputSchema: objSchema(map[string]any{
				"key":                 strProp("Optional exact cache key to fetch a single discovery entry."),
				"kind":                strProp("Optional filter matching either series X or Y kind."),
				"location_id":         strProp("Optional filter matching either series X or Y locationId."),
				"metric":              strProp("Optional filter matching either series X or Y metric."),
				"algorithm":           strProp("Optional filter by algorithm."),
				"min_abs_correlation": numProp("Optional minimum absolute correlation (or bestLagCorrelation) threshold."),
				"min_score":           numProp("Optional minimum discovery score threshold."),
				"max_p_value":         numProp("Optional maximum p-value threshold (e.g. 0.05)."),
				"limit":               intProp("Maximum entries to return (default 30, max 200)."),
				"offset":              intProp("Pagination offset (default 0)."),
			}, nil),
		},
	}
}

func (s *desktopService) toolGetCorrelationCatalog() toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is not initialized")
	}
	cfg := s.loadConfig()
	cat := analysis.BuildCatalogWithStore(s.deps.Store, cfg)
	return toolJSONResult(map[string]any{
		"catalog": cat,
		"algorithms": []string{
			"pearson", "spearman", "cross_correlation",
			"tensai_mlp", "tensai_lstm", "tensai_knn",
		},
		"rangeKeys":     []string{"30d", "90d", "1y", "3y", "5y", "10y", "30y", "all"},
		"granularities": []string{"auto", "hourly", "daily", "monthly"},
	})
}

func (s *desktopService) toolAnalyzeCorrelation(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is not initialized")
	}
	var args struct {
		XKind           string  `json:"x_kind"`
		XLocationID     string  `json:"x_location_id"`
		XMetric         string  `json:"x_metric"`
		YKind           string  `json:"y_kind"`
		YLocationID     string  `json:"y_location_id"`
		YMetric         string  `json:"y_metric"`
		RangeKey        string  `json:"range_key"`
		Granularity     string  `json:"granularity"`
		Algorithm       string  `json:"algorithm"`
		Deseasonalize   bool    `json:"deseasonalize"`
		MovingAvgWindow int     `json:"moving_avg_window"`
		Difference      bool    `json:"difference"`
		Standardize     bool    `json:"standardize"`
		Lag             int     `json:"lag"`
		MaxLag          int     `json:"max_lag"`
		LagWindow       int     `json:"lag_window"`
		HiddenSize      int     `json:"hidden_size"`
		Epochs          int     `json:"epochs"`
		LearningRate    float64 `json:"learning_rate"`
		Activation      string  `json:"activation"`
		TrainRatio      float64 `json:"train_ratio"`
		K               int     `json:"k"`
		Classes         int     `json:"classes"`
		IncludeSeries   bool    `json:"include_series"`
		MaxSeriesPoints int     `json:"max_series_points"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.XKind == "" || args.XLocationID == "" || args.XMetric == "" ||
		args.YKind == "" || args.YLocationID == "" || args.YMetric == "" {
		return toolErrorResult("x_kind, x_location_id, x_metric, y_kind, y_location_id, and y_metric are required")
	}
	if args.RangeKey == "" {
		args.RangeKey = "1y"
	}
	if args.Granularity == "" {
		args.Granularity = "auto"
	}
	if args.Algorithm == "" {
		args.Algorithm = "pearson"
	}

	req := analysis.CorrelationRequest{
		SeriesX:     analysis.SeriesSpec{Kind: args.XKind, LocationID: args.XLocationID, Metric: args.XMetric},
		SeriesY:     analysis.SeriesSpec{Kind: args.YKind, LocationID: args.YLocationID, Metric: args.YMetric},
		RangeKey:    args.RangeKey,
		Granularity: args.Granularity,
		Algorithm:   args.Algorithm,
		Preprocess: analysis.PreprocessOptions{
			Deseasonalize:   args.Deseasonalize,
			MovingAvgWindow: args.MovingAvgWindow,
			Difference:      args.Difference,
			Standardize:     args.Standardize,
			Lag:             args.Lag,
		},
		Params: analysis.AlgorithmParams{
			MaxLag:       args.MaxLag,
			LagWindow:    args.LagWindow,
			HiddenSize:   args.HiddenSize,
			Epochs:       args.Epochs,
			LearningRate: args.LearningRate,
			Activation:   args.Activation,
			TrainRatio:   args.TrainRatio,
			K:            args.K,
			Classes:      args.Classes,
		},
	}

	res, err := analysis.Analyze(s.deps.Store, req)
	if err != nil {
		return toolErrorResult(err.Error())
	}

	out := map[string]any{
		"request":            req,
		"algorithm":          res.Algorithm,
		"granularity":        res.Granularity,
		"sampleCount":        res.SampleCount,
		"trainCount":         res.TrainCount,
		"testCount":          res.TestCount,
		"warnings":           res.Warnings,
		"metrics":            res.Metrics,
		"lagProfile":         res.LagProfile,
		"featureImportances": res.FeatureImportances,
		"confusionMatrix":    res.ConfusionMatrix,
	}
	if len(res.LossHistory) > 0 {
		out["lossSummary"] = summarizeLossHistory(res.LossHistory)
	}
	if args.IncludeSeries && len(res.TimeSeries) > 0 {
		maxPts := args.MaxSeriesPoints
		if maxPts <= 0 {
			maxPts = 120
		}
		out["timeSeries"] = downsampleTimePairPoints(res.TimeSeries, maxPts)
	}
	return toolJSONResult(out)
}

func (s *desktopService) toolEvaluateCorrelationRobustness(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Store == nil {
		return toolErrorResult("store is not initialized")
	}
	var args struct {
		XKind       string `json:"x_kind"`
		XLocationID string `json:"x_location_id"`
		XMetric     string `json:"x_metric"`
		YKind       string `json:"y_kind"`
		YLocationID string `json:"y_location_id"`
		YMetric     string `json:"y_metric"`
		RangeKey    string `json:"range_key"`
		Granularity string `json:"granularity"`
		Algorithm   string `json:"algorithm"`
		MaxLag      int    `json:"max_lag"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.XKind == "" || args.XLocationID == "" || args.XMetric == "" ||
		args.YKind == "" || args.YLocationID == "" || args.YMetric == "" {
		return toolErrorResult("x_kind, x_location_id, x_metric, y_kind, y_location_id, and y_metric are required")
	}
	if args.RangeKey == "" {
		args.RangeKey = "3y"
	}
	if args.Granularity == "" {
		args.Granularity = "monthly"
	}
	if args.Algorithm == "" {
		args.Algorithm = "cross_correlation"
	}
	switch args.Algorithm {
	case "pearson", "spearman", "cross_correlation":
	default:
		return toolErrorResult("evaluate_correlation_robustness supports statistical algorithms: pearson, spearman, cross_correlation")
	}
	if args.MaxLag <= 0 {
		args.MaxLag = 12
	}

	type modeSpec struct {
		Name          string
		Deseasonalize bool
		Difference    bool
	}
	modes := []modeSpec{
		{Name: "raw", Deseasonalize: false, Difference: false},
		{Name: "deseasonalized", Deseasonalize: true, Difference: false},
		{Name: "differenced", Deseasonalize: false, Difference: true},
		{Name: "deseasonalized_and_differenced", Deseasonalize: true, Difference: true},
	}

	modeResults := make(map[string]any, len(modes))
	var rawCorr, deseasonCorr, diffCorr float64
	var deseasonP, diffP float64
	var minSamples int

	for _, m := range modes {
		req := analysis.CorrelationRequest{
			SeriesX:     analysis.SeriesSpec{Kind: args.XKind, LocationID: args.XLocationID, Metric: args.XMetric},
			SeriesY:     analysis.SeriesSpec{Kind: args.YKind, LocationID: args.YLocationID, Metric: args.YMetric},
			RangeKey:    args.RangeKey,
			Granularity: args.Granularity,
			Algorithm:   args.Algorithm,
			Preprocess: analysis.PreprocessOptions{
				Deseasonalize: m.Deseasonalize,
				Difference:    m.Difference,
			},
			Params: analysis.AlgorithmParams{
				MaxLag: args.MaxLag,
			},
		}
		res, err := analysis.Analyze(s.deps.Store, req)
		if err != nil {
			modeResults[m.Name] = map[string]any{"error": err.Error()}
			continue
		}
		if minSamples == 0 || res.SampleCount < minSamples {
			minSamples = res.SampleCount
		}
		effCorr := effectiveAbsCorr(res.Algorithm, res.Metrics)
		switch m.Name {
		case "raw":
			rawCorr = effCorr
		case "deseasonalized":
			deseasonCorr = effCorr
			deseasonP = res.Metrics.PValue
		case "differenced":
			diffCorr = effCorr
			diffP = res.Metrics.PValue
		}
		modeResults[m.Name] = map[string]any{
			"sampleCount": res.SampleCount,
			"metrics":     res.Metrics,
			"warnings":    res.Warnings,
		}
	}

	likelySeasonalSpurious := rawCorr >= 0.45 && deseasonCorr < 0.25 && (rawCorr-deseasonCorr) >= 0.25
	robustRelationship := deseasonCorr >= 0.30 && deseasonP < 0.05 && diffCorr >= 0.20 && diffP < 0.05
	lowSampleSize := minSamples > 0 && minSamples < 30

	assessment := "weak_or_inconclusive"
	if likelySeasonalSpurious {
		assessment = "likely_seasonal_spurious_correlation"
	} else if robustRelationship {
		assessment = "robust_correlation"
	} else if deseasonCorr >= 0.30 && deseasonP < 0.05 {
		assessment = "moderate_deseasonalized_correlation"
	}

	return toolJSONResult(map[string]any{
		"pair": map[string]any{
			"seriesX":     analysis.SeriesSpec{Kind: args.XKind, LocationID: args.XLocationID, Metric: args.XMetric},
			"seriesY":     analysis.SeriesSpec{Kind: args.YKind, LocationID: args.YLocationID, Metric: args.YMetric},
			"rangeKey":    args.RangeKey,
			"granularity": args.Granularity,
			"algorithm":   args.Algorithm,
		},
		"modes": modeResults,
		"diagnosis": map[string]any{
			"assessment":             assessment,
			"likelySeasonalSpurious": likelySeasonalSpurious,
			"robustRelationship":     robustRelationship,
			"lowSampleSizeWarning":   lowSampleSize,
			"minSampleCount":         minSamples,
			"rawAbsCorr":             math.Round(rawCorr*10000) / 10000,
			"deseasonalizedAbsCorr":  math.Round(deseasonCorr*10000) / 10000,
			"differencedAbsCorr":     math.Round(diffCorr*10000) / 10000,
		},
	})
}

func (s *desktopService) toolManageCorrelationDiscovery(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Discovery == nil {
		return toolErrorResult("correlation discovery manager is not initialized")
	}
	var args struct {
		Action              string   `json:"action"`
		Kinds               []string `json:"kinds"`
		LocationIDs         []string `json:"location_ids"`
		Metrics             []string `json:"metrics"`
		RangeKey            string   `json:"range_key"`
		Granularity         string   `json:"granularity"`
		Algorithms          []string `json:"algorithms"`
		DeseasonalizeModes  []bool   `json:"deseasonalize_modes"`
		Difference          bool     `json:"difference"`
		MovingAvgWindow     int      `json:"moving_avg_window"`
		MaxLag              int      `json:"max_lag"`
		MinSamples          int      `json:"min_samples"`
		SameLocationAllowed bool     `json:"same_location_allowed"`
		CrossKindOnly       bool     `json:"cross_kind_only"`
		ForceRerun          bool     `json:"force_rerun"`
		Keys                []string `json:"keys"`
	}
	if err := unmarshalArgs(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "status":
		return toolJSONResult(s.deps.Discovery.Status())
	case "start":
		cfg := analysis.DiscoveryConfig{
			Kinds:               args.Kinds,
			LocationIDs:         args.LocationIDs,
			Metrics:             args.Metrics,
			RangeKey:            args.RangeKey,
			Granularity:         args.Granularity,
			Algorithms:          args.Algorithms,
			DeseasonalizeModes:  args.DeseasonalizeModes,
			Difference:          args.Difference,
			MovingAvgWindow:     args.MovingAvgWindow,
			MaxLag:              args.MaxLag,
			MinSamples:          args.MinSamples,
			SameLocationAllowed: args.SameLocationAllowed,
			CrossKindOnly:       args.CrossKindOnly,
			ForceRerun:          args.ForceRerun,
		}
		if err := s.deps.Discovery.Start(cfg); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Discovery.Status())
	case "pause":
		if err := s.deps.Discovery.Pause(); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Discovery.Status())
	case "resume":
		if err := s.deps.Discovery.Resume(); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Discovery.Status())
	case "clear_cache":
		if err := s.deps.Discovery.ClearCached(); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{
			"cleared": true,
			"status":  s.deps.Discovery.Status(),
		})
	case "delete_cache":
		deleted, err := s.deps.Discovery.DeleteCached(args.Keys)
		if err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(map[string]any{"deleted": deleted})
	case "rerun":
		if err := s.deps.Discovery.RerunEntries(args.Keys); err != nil {
			return toolErrorResult(err.Error())
		}
		return toolJSONResult(s.deps.Discovery.Status())
	default:
		return toolErrorResult(fmt.Sprintf("unsupported action %q (expected status, start, pause, resume, clear_cache, delete_cache, or rerun)", args.Action))
	}
}

func (s *desktopService) toolListCorrelationDiscoveries(rawArgs json.RawMessage) toolCallResult {
	if s.deps.Discovery == nil {
		return toolErrorResult("correlation discovery manager is not initialized")
	}
	var args struct {
		Key               string  `json:"key"`
		Kind              string  `json:"kind"`
		LocationID        string  `json:"location_id"`
		Metric            string  `json:"metric"`
		Algorithm         string  `json:"algorithm"`
		MinAbsCorrelation float64 `json:"min_abs_correlation"`
		MinScore          float64 `json:"min_score"`
		MaxPValue         float64 `json:"max_p_value"`
		Limit             int     `json:"limit"`
		Offset            int     `json:"offset"`
	}
	_ = unmarshalArgs(rawArgs, &args)

	if strings.TrimSpace(args.Key) != "" {
		item, ok, err := s.deps.Discovery.GetCached(strings.TrimSpace(args.Key))
		if err != nil {
			return toolErrorResult(err.Error())
		}
		if !ok {
			return toolErrorResult(fmt.Sprintf("correlation discovery entry not found for key %q", args.Key))
		}
		return toolJSONResult(item)
	}

	items, err := s.deps.Discovery.ListCached()
	if err != nil {
		return toolErrorResult(err.Error())
	}

	filtered := make([]analysis.DiscoveryResultItem, 0, len(items))
	for _, it := range items {
		if args.Kind != "" && it.Request.SeriesX.Kind != args.Kind && it.Request.SeriesY.Kind != args.Kind {
			continue
		}
		if args.LocationID != "" && it.Request.SeriesX.LocationID != args.LocationID && it.Request.SeriesY.LocationID != args.LocationID {
			continue
		}
		if args.Metric != "" && it.Request.SeriesX.Metric != args.Metric && it.Request.SeriesY.Metric != args.Metric {
			continue
		}
		if args.Algorithm != "" && it.Request.Algorithm != args.Algorithm {
			continue
		}
		if args.MinAbsCorrelation > 0 && effectiveAbsCorr(it.Request.Algorithm, it.Metrics) < args.MinAbsCorrelation {
			continue
		}
		if args.MinScore > 0 && it.Score < args.MinScore {
			continue
		}
		if args.MaxPValue > 0 && it.Metrics.PValue > args.MaxPValue {
			continue
		}
		filtered = append(filtered, it)
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	offset := args.Offset
	if offset < 0 {
		offset = 0
	}
	totalMatched := len(filtered)
	if offset >= totalMatched {
		filtered = nil
	} else {
		end := offset + limit
		if end > totalMatched {
			end = totalMatched
		}
		filtered = filtered[offset:end]
	}

	return toolJSONResult(map[string]any{
		"totalCached":  len(items),
		"totalMatched": totalMatched,
		"offset":       offset,
		"limit":        limit,
		"count":        len(filtered),
		"items":        filtered,
	})
}

func effectiveAbsCorr(algo string, m analysis.CorrelationMetrics) float64 {
	if algo == "cross_correlation" {
		return math.Abs(m.BestLagCorrelation)
	}
	return math.Abs(m.Correlation)
}

func summarizeLossHistory(losses []analysis.LossPoint) map[string]any {
	if len(losses) == 0 {
		return nil
	}
	first := losses[0]
	last := losses[len(losses)-1]
	minTest := losses[0].TestLoss
	minEpoch := losses[0].Epoch
	for _, lp := range losses[1:] {
		if lp.TestLoss < minTest {
			minTest = lp.TestLoss
			minEpoch = lp.Epoch
		}
	}
	return map[string]any{
		"epochs":          len(losses),
		"initialTrain":    first.TrainLoss,
		"initialTest":     first.TestLoss,
		"finalTrain":      last.TrainLoss,
		"finalTest":       last.TestLoss,
		"bestTestLoss":    minTest,
		"bestTestEpoch":   minEpoch,
		"overfitGapFinal": last.TestLoss - last.TrainLoss,
	}
}

func downsampleTimePairPoints(pts []analysis.TimePairPoint, maxPts int) []analysis.TimePairPoint {
	if len(pts) <= maxPts || maxPts <= 0 {
		return pts
	}
	if maxPts == 1 {
		return pts[:1]
	}
	out := make([]analysis.TimePairPoint, 0, maxPts)
	step := float64(len(pts)-1) / float64(maxPts-1)
	for i := 0; i < maxPts; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx >= len(pts) {
			idx = len(pts) - 1
		}
		out = append(out, pts[idx])
	}
	return out
}
