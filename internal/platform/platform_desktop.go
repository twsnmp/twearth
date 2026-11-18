//go:build !android && !ios

package platform

import (
	"os"
	"path/filepath"
)

const (
	isMobile      = false
	canSelfUpdate = true
)

// DataDir returns the application data directory on desktop platforms.
func DataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "twearth")
}
