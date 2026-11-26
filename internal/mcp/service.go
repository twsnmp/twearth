package mcp

import (
	"context"

	"github.com/twsnmp/twearth/internal/analysis"
	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

const DefaultPort = 18088

// Status describes the current runtime state of the local-only MCP server.
type Status struct {
	Supported bool   `json:"supported"`
	Running   bool   `json:"running"`
	Port      int    `json:"port"`
	Endpoint  string `json:"endpoint,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Dependencies provides backend access required by MCP tool handlers.
type Dependencies struct {
	Store           *store.Store
	Collector       *collector.Manager
	Discovery       *analysis.DiscoveryManager
	Ranking         *analysis.RankingManager
	Version         string
	OnConfigUpdated func(store.Config)
}

// Service manages the lifecycle of the local-only MCP server.
type Service interface {
	Configure(ctx context.Context, enabled bool, port int) error
	Stop(ctx context.Context) error
	GetStatus() Status
}
