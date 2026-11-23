//go:build android || ios

package mcp

import (
	"context"
	"fmt"
)

type mobileService struct{}

// NewService creates a no-op MCP service on mobile platforms.
func NewService(_ Dependencies) Service {
	return &mobileService{}
}

func (s *mobileService) Configure(_ context.Context, enabled bool, _ int) error {
	if enabled {
		return fmt.Errorf("MCP server is only supported on desktop platforms")
	}
	return nil
}

func (s *mobileService) Stop(_ context.Context) error {
	return nil
}

func (s *mobileService) GetStatus() Status {
	return Status{
		Supported: false,
		Running:   false,
	}
}
