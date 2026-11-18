//go:build android || ios

package updater

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type mobileService struct {
	currentVersion string
}

// NewService creates a no-op updater service on mobile platforms where store updates are used.
func NewService() Service {
	return &mobileService{}
}

func (s *mobileService) Init(_ *application.App, currentVersion string, _ bool) error {
	s.currentVersion = currentVersion
	return nil
}

func (s *mobileService) Check(_ context.Context) Status {
	return s.GetStatus()
}

func (s *mobileService) DownloadAndInstall(_ context.Context) error {
	return fmt.Errorf("self-update is not supported on mobile platforms")
}

func (s *mobileService) Restart(_ context.Context) error {
	return fmt.Errorf("self-update restart is not supported on mobile platforms")
}

func (s *mobileService) GetStatus() Status {
	return Status{
		Supported:      false,
		State:          "unsupported",
		CurrentVersion: s.currentVersion,
	}
}
