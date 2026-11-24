package app

import (
	"fmt"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/platform"
)

// GetCorrelationSeriesCatalog returns selectable weather/marine locations and metrics (desktop only).
func (s *AppService) GetCorrelationSeriesCatalog() (analysis.SeriesCatalog, error) {
	if platform.GetInfo().IsMobile {
		return analysis.SeriesCatalog{}, fmt.Errorf("correlation analysis is only supported on desktop")
	}
	cfg, err := s.store.GetConfig()
	if err != nil {
		return analysis.SeriesCatalog{}, err
	}
	return analysis.BuildCatalog(cfg), nil
}

// AnalyzeCorrelation executes statistical or tensai ML correlation analysis between two series (desktop only).
func (s *AppService) AnalyzeCorrelation(req analysis.CorrelationRequest) (analysis.CorrelationResult, error) {
	if platform.GetInfo().IsMobile {
		return analysis.CorrelationResult{}, fmt.Errorf("correlation analysis is only supported on desktop")
	}
	return analysis.Analyze(s.store, req)
}
