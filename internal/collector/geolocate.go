package collector

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GeoLocationResult represents an approximate location resolved via IP geolocation.
type GeoLocationResult struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	City      string  `json:"city"`
	Region    string  `json:"region"`
	Country   string  `json:"country"`
	Source    string  `json:"source"` // "ip"
}

// DetectLocationByIP resolves approximate coordinates and city name using keyless HTTPS IP geolocation APIs.
func DetectLocationByIP(ctx context.Context) (GeoLocationResult, error) {
	client := newHTTPClient(10 * time.Second)

	// Primary: ipwho.is (no API key, HTTPS)
	if res, err := fetchIPWhoIs(ctx, client); err == nil {
		return res, nil
	}

	// Fallback: ipapi.co (no API key, HTTPS)
	if res, err := fetchIPApiCo(ctx, client); err == nil {
		return res, nil
	}

	return GeoLocationResult{}, fmt.Errorf("ip geolocation failed on all providers")
}

func fetchIPWhoIs(ctx context.Context, client *http.Client) (GeoLocationResult, error) {
	var resp struct {
		Success   bool    `json:"success"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		City      string  `json:"city"`
		Region    string  `json:"region"`
		Country   string  `json:"country"`
	}
	if err := fetchJSON(ctx, client, "https://ipwho.is/", &resp); err != nil {
		return GeoLocationResult{}, err
	}
	if !resp.Success || (resp.Latitude == 0 && resp.Longitude == 0) {
		return GeoLocationResult{}, fmt.Errorf("ipwho.is returned unsuccessful response")
	}
	return GeoLocationResult{
		Latitude:  resp.Latitude,
		Longitude: resp.Longitude,
		City:      strings.TrimSpace(resp.City),
		Region:    strings.TrimSpace(resp.Region),
		Country:   strings.TrimSpace(resp.Country),
		Source:    "ip",
	}, nil
}

func fetchIPApiCo(ctx context.Context, client *http.Client) (GeoLocationResult, error) {
	var resp struct {
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		City        string  `json:"city"`
		Region      string  `json:"region"`
		CountryName string  `json:"country_name"`
	}
	if err := fetchJSON(ctx, client, "https://ipapi.co/json/", &resp); err != nil {
		return GeoLocationResult{}, err
	}
	if resp.Latitude == 0 && resp.Longitude == 0 {
		return GeoLocationResult{}, fmt.Errorf("ipapi.co returned empty coordinates")
	}
	return GeoLocationResult{
		Latitude:  resp.Latitude,
		Longitude: resp.Longitude,
		City:      strings.TrimSpace(resp.City),
		Region:    strings.TrimSpace(resp.Region),
		Country:   strings.TrimSpace(resp.CountryName),
		Source:    "ip",
	}, nil
}
