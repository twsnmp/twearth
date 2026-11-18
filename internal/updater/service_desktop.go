//go:build !android && !ios

package updater

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

const defaultGitHubRepo = "twsnmp/twearth"

type desktopService struct {
	mu             sync.RWMutex
	app            *application.App
	currentVersion string
	latestVersion  string
	releaseNotes   string
	lastErr        string
}

// NewService creates a desktop self-update service backed by Wails v3 app.Updater.
func NewService() Service {
	return &desktopService{}
}

func (s *desktopService) Init(app *application.App, currentVersion string, autoCheck bool) error {
	gh, err := github.New(github.Config{
		Repository:    defaultGitHubRepo,
		ChecksumAsset: "checksums.txt",
	})
	if err != nil {
		return fmt.Errorf("create github updater provider: %w", err)
	}

	cfg := wailsupdater.Config{
		CurrentVersion: currentVersion,
		Providers:      []wailsupdater.Provider{gh},
	}
	if autoCheck {
		cfg.CheckInterval = 6 * time.Hour
	}

	if err := app.Updater.Init(cfg); err != nil {
		return fmt.Errorf("init wails updater: %w", err)
	}

	s.mu.Lock()
	s.app = app
	s.currentVersion = currentVersion
	s.mu.Unlock()

	if autoCheck {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = s.Check(ctx)
		}()
	}

	return nil
}

func (s *desktopService) Check(ctx context.Context) Status {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return s.GetStatus()
	}

	rel, err := app.Updater.Check(ctx)
	s.mu.Lock()
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.lastErr = ""
		if rel != nil {
			s.latestVersion = rel.Version
			s.releaseNotes = rel.Notes
		}
	}
	s.mu.Unlock()

	return s.GetStatus()
}

func (s *desktopService) DownloadAndInstall(ctx context.Context) error {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return fmt.Errorf("updater not initialized")
	}
	err := app.Updater.DownloadAndInstall(ctx)
	s.mu.Lock()
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.lastErr = ""
	}
	s.mu.Unlock()
	return err
}

func (s *desktopService) Restart(ctx context.Context) error {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return fmt.Errorf("updater not initialized")
	}
	return app.Updater.Restart(ctx)
}

func (s *desktopService) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := string(wailsupdater.StateUnconfigured)
	if s.app != nil {
		state = string(s.app.Updater.State())
	}

	return Status{
		Supported:      true,
		State:          state,
		CurrentVersion: s.currentVersion,
		LatestVersion:  s.latestVersion,
		ReleaseNotes:   s.releaseNotes,
		Error:          s.lastErr,
	}
}
