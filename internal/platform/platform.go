package platform

import "runtime"

// Info describes the runtime platform capabilities exposed to the frontend.
type Info struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	IsMobile      bool   `json:"isMobile"`
	CanSelfUpdate bool   `json:"canSelfUpdate"`
	DataDir       string `json:"dataDir"`
}

// GetInfo returns platform metadata and capability flags for the current build target.
func GetInfo() Info {
	return Info{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		IsMobile:      isMobile,
		CanSelfUpdate: canSelfUpdate,
		DataDir:       DataDir(),
	}
}
