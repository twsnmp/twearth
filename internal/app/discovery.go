package app

import (
	"fmt"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/platform"
)

// StartCorrelationDiscovery starts a new background correlation discovery job (desktop only).
func (s *AppService) StartCorrelationDiscovery(cfg analysis.DiscoveryConfig) error {
	if platform.GetInfo().IsMobile {
		return fmt.Errorf("correlation discovery is only supported on desktop")
	}
	if s.discovery == nil {
		return fmt.Errorf("discovery manager is not initialized")
	}
	return s.discovery.Start(cfg)
}

// PauseCorrelationDiscovery pauses the active background discovery job and saves its cursor.
func (s *AppService) PauseCorrelationDiscovery() error {
	if platform.GetInfo().IsMobile || s.discovery == nil {
		return nil
	}
	return s.discovery.Pause()
}

// ResumeCorrelationDiscovery resumes a paused or interrupted background discovery job.
func (s *AppService) ResumeCorrelationDiscovery() error {
	if platform.GetInfo().IsMobile {
		return fmt.Errorf("correlation discovery is only supported on desktop")
	}
	if s.discovery == nil {
		return fmt.Errorf("discovery manager is not initialized")
	}
	return s.discovery.Resume()
}

// GetCorrelationDiscoveryStatus returns the current background discovery progress and config.
func (s *AppService) GetCorrelationDiscoveryStatus() (analysis.DiscoveryProgress, error) {
	if platform.GetInfo().IsMobile {
		return analysis.DiscoveryProgress{}, fmt.Errorf("correlation discovery is only supported on desktop")
	}
	if s.discovery == nil {
		return analysis.DiscoveryProgress{}, fmt.Errorf("discovery manager is not initialized")
	}
	return s.discovery.Status(), nil
}

// ListCorrelationCache returns all remembered correlation discovery results sorted by score descending.
func (s *AppService) ListCorrelationCache() ([]analysis.DiscoveryResultItem, error) {
	if platform.GetInfo().IsMobile {
		return nil, fmt.Errorf("correlation discovery is only supported on desktop")
	}
	if s.discovery == nil {
		return nil, nil
	}
	return s.discovery.ListCached()
}

// GetCorrelationCacheEntry returns a single remembered correlation entry by key.
func (s *AppService) GetCorrelationCacheEntry(key string) (*analysis.DiscoveryResultItem, error) {
	if platform.GetInfo().IsMobile || s.discovery == nil {
		return nil, nil
	}
	item, ok, err := s.discovery.GetCached(key)
	if err != nil || !ok {
		return nil, err
	}
	return &item, nil
}

// DeleteCorrelationCacheEntries deletes specific remembered correlation entries by key.
func (s *AppService) DeleteCorrelationCacheEntries(keys []string) (int, error) {
	if platform.GetInfo().IsMobile || s.discovery == nil {
		return 0, nil
	}
	return s.discovery.DeleteCached(keys)
}

// ClearCorrelationCache deletes all remembered correlation entries.
func (s *AppService) ClearCorrelationCache() error {
	if platform.GetInfo().IsMobile || s.discovery == nil {
		return nil
	}
	return s.discovery.ClearCached()
}

// RerunCorrelationEntries clears the specified keys and recomputes them in the background.
func (s *AppService) RerunCorrelationEntries(keys []string) error {
	if platform.GetInfo().IsMobile {
		return fmt.Errorf("correlation discovery is only supported on desktop")
	}
	if s.discovery == nil {
		return fmt.Errorf("discovery manager is not initialized")
	}
	return s.discovery.RerunEntries(keys)
}

// OpenCorrelationDetail opens the detailed Correlation & ML Analysis window for a cached discovery item.
// If forceNewWindow is false and an existing correlation window is open, it reuses and focuses that window.
func (s *AppService) OpenCorrelationDetail(key string, forceNewWindow bool) error {
	if platform.GetInfo().IsMobile || s.app == nil {
		return fmt.Errorf("multi-window is only supported on desktop")
	}
	if s.discovery == nil {
		return fmt.Errorf("discovery manager is not initialized")
	}
	item, ok, err := s.discovery.GetCached(key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("correlation cache entry not found: %s", key)
	}

	if !forceNewWindow {
		s.winMu.Lock()
		var existing *analysisWindowEntry
		for _, e := range s.windows {
			if e != nil && e.win != nil && e.view == "correlation" {
				if existing == nil || e.order > existing.order {
					existing = e
				}
			}
		}
		s.winMu.Unlock()

		if existing != nil {
			existing.win.Show()
			existing.win.Focus()
			s.app.Event.Emit("twearth:correlation-inspect", item.Request)
			return nil
		}
	}

	return s.OpenAnalysisWindowWithOptions(AnalysisWindowOptions{
		View:     "correlation",
		CacheKey: key,
	})
}

// ShutdownCorrelationDiscovery pauses any active discovery worker cleanly during app shutdown.
func (s *AppService) ShutdownCorrelationDiscovery() {
	if s.discovery != nil {
		s.discovery.Shutdown()
	}
}
