package app

import (
	"fmt"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
)

// GetMunicipalityRankingCatalog returns available ranking factors, regions, prefectures, and custom municipalities.
func (s *AppService) GetMunicipalityRankingCatalog() (analysis.RankingCatalog, error) {
	if platform.GetInfo().IsMobile {
		return analysis.RankingCatalog{}, fmt.Errorf("municipality ranking is only supported on desktop")
	}
	return analysis.BuildRankingCatalog(s.store), nil
}

// StartMunicipalityRanking starts a new municipality safety & livability ranking analysis job.
func (s *AppService) StartMunicipalityRanking(cfg analysis.RankingConfig) error {
	if platform.GetInfo().IsMobile {
		return fmt.Errorf("municipality ranking is only supported on desktop")
	}
	if s.ranking == nil {
		return fmt.Errorf("ranking manager is not initialized")
	}
	return s.ranking.Start(cfg)
}

// PauseMunicipalityRanking pauses the active municipality ranking job and saves its cursor.
func (s *AppService) PauseMunicipalityRanking() error {
	if platform.GetInfo().IsMobile || s.ranking == nil {
		return nil
	}
	return s.ranking.Pause()
}

// ResumeMunicipalityRanking resumes a paused or interrupted municipality ranking job.
func (s *AppService) ResumeMunicipalityRanking() error {
	if platform.GetInfo().IsMobile {
		return fmt.Errorf("municipality ranking is only supported on desktop")
	}
	if s.ranking == nil {
		return fmt.Errorf("ranking manager is not initialized")
	}
	return s.ranking.Resume()
}

// GetMunicipalityRankingStatus returns the current municipality ranking progress and config.
func (s *AppService) GetMunicipalityRankingStatus() (analysis.RankingProgress, error) {
	if platform.GetInfo().IsMobile {
		return analysis.RankingProgress{}, fmt.Errorf("municipality ranking is only supported on desktop")
	}
	if s.ranking == nil {
		return analysis.RankingProgress{}, fmt.Errorf("ranking manager is not initialized")
	}
	return s.ranking.Status(), nil
}

// ListMunicipalityRankingResults returns all cached municipality ranking results sorted by total score descending.
func (s *AppService) ListMunicipalityRankingResults() ([]analysis.RankingResultItem, error) {
	if platform.GetInfo().IsMobile || s.ranking == nil {
		return nil, nil
	}
	return s.ranking.ListResults()
}

// ClearMunicipalityRankingCache clears cached ranking results (and optionally climate cache).
func (s *AppService) ClearMunicipalityRankingCache(clearClimate bool) error {
	if platform.GetInfo().IsMobile || s.ranking == nil {
		return nil
	}
	return s.ranking.ClearCache(clearClimate)
}

// AddCustomMunicipality registers a custom municipality/city into the ranking catalog.
func (s *AppService) AddCustomMunicipality(entry store.MunicipalityEntry) (store.MunicipalityEntry, error) {
	if s.store == nil {
		return entry, fmt.Errorf("store is not initialized")
	}
	return s.store.SaveCustomMunicipality(entry)
}

// RemoveCustomMunicipality deletes a user-registered custom municipality from the ranking catalog.
func (s *AppService) RemoveCustomMunicipality(id string) error {
	if s.store == nil {
		return nil
	}
	return s.store.DeleteCustomMunicipality(id)
}

// ShutdownMunicipalityRanking pauses any active ranking worker cleanly during app shutdown.
func (s *AppService) ShutdownMunicipalityRanking() {
	if s.ranking != nil {
		s.ranking.Shutdown()
	}
}
