package collector

import (
	"context"
	"net/http"
	"time"
)

// SpaceClient fetches NOAA SWPC geomagnetic Kp index and GOES X-ray flux.
type SpaceClient struct {
	Client   *http.Client
	KpURL    string
	XRayURL  string
}

// NewSpaceClient creates a SpaceClient with NOAA SWPC endpoints.
func NewSpaceClient() *SpaceClient {
	return &SpaceClient{
		Client:  newHTTPClient(15 * time.Second),
		KpURL:   "https://services.swpc.noaa.gov/json/planetary_k_index_1m.json",
		XRayURL: "https://services.swpc.noaa.gov/json/goes/primary/xrays-6-hour.json",
	}
}

type noaaKpEntry struct {
	TimeTag string  `json:"time_tag"`
	KpIndex float64 `json:"kp_index"`
}

type noaaXRayEntry struct {
	TimeTag string  `json:"time_tag"`
	Flux    float64 `json:"flux"`
	Energy  string  `json:"energy"`
}

// FetchSpaceWeather retrieves the latest Kp index and GOES X-ray flare class.
func (c *SpaceClient) FetchSpaceWeather(ctx context.Context) (SpaceWeatherSnapshot, error) {
	snap := SpaceWeatherSnapshot{
		Timestamp:  time.Now().UTC(),
		FlareClass: "A",
	}

	var kps []noaaKpEntry
	if err := fetchJSON(ctx, c.Client, c.KpURL, &kps); err != nil {
		return SpaceWeatherSnapshot{}, err
	}
	if len(kps) > 0 {
		snap.KpIndex = kps[len(kps)-1].KpIndex
	}

	var xrays []noaaXRayEntry
	if err := fetchJSON(ctx, c.Client, c.XRayURL, &xrays); err == nil && len(xrays) > 0 {
		for i := len(xrays) - 1; i >= 0; i-- {
			if xrays[i].Energy == "0.1-0.8nm" || xrays[i].Energy == "" {
				snap.XRayFlux = xrays[i].Flux
				snap.FlareClass = classifyXRayFlux(xrays[i].Flux)
				break
			}
		}
	}

	return snap, nil
}

func classifyXRayFlux(flux float64) string {
	switch {
	case flux >= 1e-4:
		return "X"
	case flux >= 1e-5:
		return "M"
	case flux >= 1e-6:
		return "C"
	case flux >= 1e-7:
		return "B"
	default:
		return "A"
	}
}
