package analysis

import (
	"fmt"
	"sync"

	"github.com/twsnmp/twearth/internal/store"
)

// RawSeriesPoint represents a single timestamped observation from a SeriesProvider.
type RawSeriesPoint struct {
	Timestamp string
	Label     string
	Value     float64
}

// CatalogKind describes a selectable time-series data domain (e.g. "weather", "marine", future "quake", "river").
type CatalogKind struct {
	ID            string `json:"id"`
	DefaultMetric string `json:"defaultMetric"`
}

// SeriesProvider abstracts a time-series data source so correlation analysis and discovery
// can be extended to additional domains (weather, marine, quake, river, space, etc.) uniformly.
type SeriesProvider interface {
	Kind() CatalogKind
	ListLocations(s *store.Store, cfg store.Config) ([]CatalogLocation, error)
	Metrics() []CatalogMetric
	LoadSeries(s *store.Store, locationID, metric, fromTS, toTS, granularity string) ([]RawSeriesPoint, error)
}

var (
	providerMu sync.RWMutex
	providers  []SeriesProvider
)

func init() {
	RegisterProvider(weatherProvider{})
	RegisterProvider(marineProvider{})
}

// RegisterProvider registers a SeriesProvider for correlation analysis and discovery.
// If a provider with the same Kind ID already exists, it is replaced.
func RegisterProvider(p SeriesProvider) {
	if p == nil {
		return
	}
	providerMu.Lock()
	defer providerMu.Unlock()
	id := p.Kind().ID
	for i, existing := range providers {
		if existing.Kind().ID == id {
			providers[i] = p
			return
		}
	}
	providers = append(providers, p)
}

// RegisteredProviders returns all registered SeriesProviders in registration order.
func RegisteredProviders() []SeriesProvider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	out := make([]SeriesProvider, len(providers))
	copy(out, providers)
	return out
}

// GetProvider looks up a registered SeriesProvider by kind ID.
func GetProvider(kind string) (SeriesProvider, bool) {
	providerMu.RLock()
	defer providerMu.RUnlock()
	for _, p := range providers {
		if p.Kind().ID == kind {
			return p, true
		}
	}
	return nil, false
}

type weatherProvider struct{}

func (weatherProvider) Kind() CatalogKind {
	return CatalogKind{ID: "weather", DefaultMetric: "temperature2m"}
}

func (weatherProvider) ListLocations(_ *store.Store, cfg store.Config) ([]CatalogLocation, error) {
	out := make([]CatalogLocation, 0, len(cfg.Locations))
	for _, l := range cfg.Locations {
		out = append(out, CatalogLocation{
			ID:     l.ID,
			Kind:   "weather",
			Name:   l.Name,
			NameEn: l.NameEn,
		})
	}
	return out, nil
}

func (weatherProvider) Metrics() []CatalogMetric {
	return []CatalogMetric{
		{ID: "temperature2m", Kind: "weather", Unit: "°C"},
		{ID: "pressureMsl", Kind: "weather", Unit: "hPa"},
		{ID: "precipitation", Kind: "weather", Unit: "mm"},
		{ID: "snowfall", Kind: "weather", Unit: "cm"},
		{ID: "windSpeed10m", Kind: "weather", Unit: "km/h"},
		{ID: "cape", Kind: "weather", Unit: "J/kg"},
	}
}

func (weatherProvider) LoadSeries(s *store.Store, locationID, metric, fromTS, toTS, granularity string) ([]RawSeriesPoint, error) {
	return queryStoreHistorySeries(s, "weather", locationID, metric, fromTS, toTS, granularity)
}

type marineProvider struct{}

func (marineProvider) Kind() CatalogKind {
	return CatalogKind{ID: "marine", DefaultMetric: "seaSurfaceTemperature"}
}

func (marineProvider) ListLocations(_ *store.Store, cfg store.Config) ([]CatalogLocation, error) {
	out := make([]CatalogLocation, 0, len(cfg.MarineLocations))
	for _, m := range cfg.MarineLocations {
		out = append(out, CatalogLocation{
			ID:       m.ID,
			Kind:     "marine",
			Name:     m.Name,
			NameEn:   m.NameEn,
			Category: m.Category,
			Region:   m.Region,
		})
	}
	return out, nil
}

func (marineProvider) Metrics() []CatalogMetric {
	return []CatalogMetric{
		{ID: "seaSurfaceTemperature", Kind: "marine", Unit: "°C"},
		{ID: "waveHeight", Kind: "marine", Unit: "m"},
		{ID: "oceanCurrentVelocity", Kind: "marine", Unit: "km/h"},
	}
}

func (marineProvider) LoadSeries(s *store.Store, locationID, metric, fromTS, toTS, granularity string) ([]RawSeriesPoint, error) {
	return queryStoreHistorySeries(s, "marine", locationID, metric, fromTS, toTS, granularity)
}

func queryStoreHistorySeries(s *store.Store, kind, locationID, metric, fromTS, toTS, granularity string) ([]RawSeriesPoint, error) {
	if s == nil {
		return nil, fmt.Errorf("store is not initialized")
	}
	res, err := s.QueryHistory(store.HistoryQuery{
		Kind:            kind,
		LocationID:      locationID,
		FromTS:          fromTS,
		ToTS:            toTS,
		Metric:          metric,
		Granularity:     granularity,
		MovingAvgWindow: 1,
	})
	if err != nil {
		return nil, err
	}
	out := make([]RawSeriesPoint, 0, len(res.Points))
	for _, pt := range res.Points {
		out = append(out, RawSeriesPoint{
			Timestamp: pt.Timestamp,
			Label:     pt.Label,
			Value:     pt.Value,
		})
	}
	return out, nil
}
