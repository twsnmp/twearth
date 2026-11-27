package ai

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/twsnmp/twearth/internal/ai/gemini"
	"github.com/twsnmp/twearth/internal/ai/model"
	"github.com/twsnmp/twearth/internal/ai/tensai"
	"github.com/twsnmp/twearth/internal/store"
)

// ChatMessage represents a single turn in a Q&A conversation.
type ChatMessage struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// LLMResponse is returned to the frontend from AI explanation and Q&A calls.
type LLMResponse struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

var (
	tensaiCacheMu    sync.Mutex
	tensaiCachedPath string
	tensaiCachedLLM  *tensai.TensaiLLM
)

// GetLLM instantiates or returns a cached langchaingo llms.Model according to cfg.
func GetLLM(ctx context.Context, cfg store.Config) (llms.Model, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLMProvider))
	switch provider {
	case "", "none":
		return nil, fmt.Errorf("LLM provider is not configured")
	case "tensai", "embedded", "local":
		modelPath, err := model.FindModel("", cfg.LLMModel)
		if err != nil {
			log.Printf("GetLLM tensai: local model %q not found: %v", cfg.LLMModel, err)
			return nil, fmt.Errorf("local model not found (%s): %w", cfg.LLMModel, err)
		}
		tensaiCacheMu.Lock()
		defer tensaiCacheMu.Unlock()
		if tensaiCachedLLM != nil && tensaiCachedPath == modelPath {
			return tensaiCachedLLM, nil
		}
		llm, err := tensai.NewWithOptions(modelPath, false)
		if err != nil {
			log.Printf("GetLLM tensai: failed to load model %s: %v", modelPath, err)
			return nil, err
		}
		tensaiCachedPath = modelPath
		tensaiCachedLLM = llm
		return llm, nil
	case "ollama":
		baseURL := "http://localhost:11434"
		if strings.TrimSpace(cfg.LLMBaseURL) != "" {
			baseURL = strings.TrimSpace(cfg.LLMBaseURL)
		}
		opts := []ollama.Option{
			ollama.WithServerURL(baseURL),
		}
		if strings.TrimSpace(cfg.LLMModel) != "" {
			opts = append(opts, ollama.WithModel(strings.TrimSpace(cfg.LLMModel)))
		}
		return ollama.New(opts...)
	case "gemini", "googleai":
		opts := []gemini.Option{
			gemini.WithAPIKey(cfg.LLMAPIKey),
		}
		if strings.TrimSpace(cfg.LLMBaseURL) != "" {
			opts = append(opts, gemini.WithBaseURL(cfg.LLMBaseURL))
		}
		if strings.TrimSpace(cfg.LLMModel) != "" {
			opts = append(opts, gemini.WithModel(cfg.LLMModel))
		}
		return gemini.New(opts...)
	case "openai":
		opts := []openai.Option{}
		if strings.TrimSpace(cfg.LLMModel) != "" {
			opts = append(opts, openai.WithModel(strings.TrimSpace(cfg.LLMModel)))
		}
		if strings.TrimSpace(cfg.LLMAPIKey) != "" {
			opts = append(opts, openai.WithToken(strings.TrimSpace(cfg.LLMAPIKey)))
		}
		if strings.TrimSpace(cfg.LLMBaseURL) != "" {
			opts = append(opts, openai.WithBaseURL(strings.TrimSpace(cfg.LLMBaseURL)))
		}
		return openai.New(opts...)
	case "anthropic", "claude":
		opts := []anthropic.Option{}
		if strings.TrimSpace(cfg.LLMModel) != "" {
			opts = append(opts, anthropic.WithModel(strings.TrimSpace(cfg.LLMModel)))
		}
		if strings.TrimSpace(cfg.LLMAPIKey) != "" {
			opts = append(opts, anthropic.WithToken(strings.TrimSpace(cfg.LLMAPIKey)))
		}
		if strings.TrimSpace(cfg.LLMBaseURL) != "" {
			opts = append(opts, anthropic.WithBaseURL(strings.TrimSpace(cfg.LLMBaseURL)))
		}
		return anthropic.New(opts...)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.LLMProvider)
	}
}

// AskSingle executes a single system + human prompt call.
func AskSingle(ctx context.Context, cfg store.Config, system, prompt string) LLMResponse {
	return AskChat(ctx, cfg, system, []ChatMessage{{Role: "user", Content: prompt}})
}

// AskChat executes a multi-turn conversation against the configured LLM.
func AskChat(ctx context.Context, cfg store.Config, system string, messages []ChatMessage) LLMResponse {
	llm, err := GetLLM(ctx, cfg)
	if err != nil {
		return LLMResponse{Error: err.Error()}
	}

	history := make([]llms.MessageContent, 0, len(messages)+1)
	if strings.TrimSpace(system) != "" {
		history = append(history, llms.TextParts(llms.ChatMessageTypeSystem, system))
	}
	for _, m := range messages {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(m.Role) {
		case "assistant", "ai", "model":
			history = append(history, llms.TextParts(llms.ChatMessageTypeAI, content))
		default:
			history = append(history, llms.TextParts(llms.ChatMessageTypeHuman, content))
		}
	}
	if len(history) == 0 {
		return LLMResponse{Error: "empty prompt"}
	}

	resp, err := llm.GenerateContent(ctx, history)
	if err != nil {
		log.Printf("LLM GenerateContent err=%v", err)
		return LLMResponse{Error: err.Error()}
	}
	if len(resp.Choices) < 1 {
		return LLMResponse{Error: "no response from LLM"}
	}
	return LLMResponse{Content: strings.TrimSpace(resp.Choices[0].Content)}
}
