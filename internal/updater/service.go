package updater

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Status represents the current self-update state exposed to the frontend.
type Status struct {
	Supported      bool   `json:"supported"`
	State          string `json:"state"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	ReleaseNotes   string `json:"releaseNotes,omitempty"`
	Error          string `json:"error,omitempty"`
}

// Service abstracts desktop self-update operations so mobile builds can omit the updater.
type Service interface {
	Init(app *application.App, currentVersion string, autoCheck bool) error
	Check(ctx context.Context) Status
	DownloadAndInstall(ctx context.Context) error
	Restart(ctx context.Context) error
	GetStatus() Status
}
