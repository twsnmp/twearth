package app

import (
	"context"
	"strings"
	"time"

	"github.com/twsnmp/twearth/internal/ai"
	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

// AskRequest represents an interactive Q&A request from the frontend.
type AskRequest struct {
	Messages     []ai.ChatMessage `json:"messages"`
	LocationID   string           `json:"locationId,omitempty"`
	ExtraContext string           `json:"extraContext,omitempty"`
	Lang         string           `json:"lang,omitempty"`
}

func (s *AppService) loadAIConfigAndState() (store.Config, collector.LatestState, error) {
	cfg, err := s.store.GetConfig()
	if err != nil {
		cfg = store.DefaultConfig()
	}
	var st collector.LatestState
	if s.collector != nil {
		st = s.collector.GetLatestState()
	}
	return cfg, st, nil
}

func llmTimeoutForProvider(provider string) time.Duration {
	if strings.EqualFold(strings.TrimSpace(provider), "tensai") {
		return 15 * time.Minute
	}
	return 3 * time.Minute
}

// LLMExplainLocation generates an AI explanation of the current state for a registered location.
func (s *AppService) LLMExplainLocation(locationID, lang string) ai.LLMResponse {
	cfg, st, _ := s.loadAIConfigAndState()
	system, prompt := ai.BuildLocationPrompt(cfg, st, s.store, locationID, lang)
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeoutForProvider(cfg.LLMProvider))
	defer cancel()
	return ai.AskSingle(ctx, cfg, system, prompt)
}

// LLMExplainView generates an AI explanation for the currently displayed UI screen.
func (s *AppService) LLMExplainView(req ai.ViewExplainRequest) ai.LLMResponse {
	cfg, st, _ := s.loadAIConfigAndState()
	system, prompt := ai.BuildViewPrompt(cfg, st, s.store, req)
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeoutForProvider(cfg.LLMProvider))
	defer cancel()
	return ai.AskSingle(ctx, cfg, system, prompt)
}

// LLMExplainEvent generates an AI explanation for a specific alert or natural event.
func (s *AppService) LLMExplainEvent(ev store.EventRecord, lang string) ai.LLMResponse {
	cfg, _, _ := s.loadAIConfigAndState()
	system, prompt := ai.BuildEventPrompt(cfg, ev, lang)
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeoutForProvider(cfg.LLMProvider))
	defer cancel()
	return ai.AskSingle(ctx, cfg, system, prompt)
}

// LLMAsk handles interactive multi-turn Q&A with real-time observation context.
func (s *AppService) LLMAsk(req AskRequest) ai.LLMResponse {
	cfg, st, _ := s.loadAIConfigAndState()
	system := ai.BuildQASystemPrompt(cfg, st, req.LocationID, req.Lang)
	if strings.TrimSpace(req.ExtraContext) != "" {
		system += "\n\n## Current Screen / Explanation Context:\n" + strings.TrimSpace(req.ExtraContext)
	}
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeoutForProvider(cfg.LLMProvider))
	defer cancel()
	return ai.AskChat(ctx, cfg, system, req.Messages)
}
