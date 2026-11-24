package analysis

import (
	"fmt"

	"github.com/twsnmp/twearth/internal/store"
)

// Analyze runs the requested statistical or tensai ML correlation analysis on store time series.
func Analyze(s *store.Store, req CorrelationRequest) (CorrelationResult, error) {
	if req.Algorithm == "" {
		req.Algorithm = "pearson"
	}
	pairs, granularity, warnings, err := LoadAlignedSeries(s, req)
	if err != nil {
		return CorrelationResult{}, err
	}
	return AnalyzePairs(req, pairs, granularity, warnings)
}

// AnalyzePairs executes the configured algorithm on pre-aligned pairs.
func AnalyzePairs(req CorrelationRequest, pairs []TimePairPoint, granularity string, warnings []string) (CorrelationResult, error) {
	switch req.Algorithm {
	case "pearson", "spearman", "cross_correlation":
		return RunStatistical(req, pairs, granularity, warnings)
	case "tensai_mlp", "tensai_lstm", "tensai_knn":
		return RunTensaiML(req, pairs, granularity, warnings)
	default:
		return CorrelationResult{}, fmt.Errorf("unsupported algorithm: %s", req.Algorithm)
	}
}
