//go:build android || ios

package platform

import (
	"os"
	"path/filepath"
)

const (
	isMobile      = true
	canSelfUpdate = false
)

// DataDir returns the sandboxed application data directory on mobile platforms.
func DataDir() string {
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "twearth")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Library", "Application Support", "twearth")
	}
	return filepath.Join(os.TempDir(), "twearth")
}
