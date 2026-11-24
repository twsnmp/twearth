package tensai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// TensaiLLM implements langchaingo's llms.Model for embedded inference using tensai.
type TensaiLLM struct {
	modelPath string
	engine    *Engine
	accType   AccelerationType
	accDetail string
}

// New creates a new TensaiLLM instance with automatic GPU -> SIMD -> CPU fallback.
func New(modelPath string) (*TensaiLLM, error) {
	return NewWithOptions(modelPath, false)
}

// NewWithOptions creates a new TensaiLLM instance with optional GPU bypass.
func NewWithOptions(modelPath string, noGPU bool) (*TensaiLLM, error) {
	if modelPath == "" {
		return nil, errors.New("model path cannot be empty")
	}

	accType, accDetail := DetectAccelerationWithOptions(noGPU)

	engine, err := LoadEngineWithOptions(modelPath, noGPU)
	if err != nil {
		log.Printf("TensaiLLM: LoadEngineWithOptions failed (model=%s): %v", modelPath, err)
		return nil, fmt.Errorf("failed to load local LLM model %q: %w", modelPath, err)
	}

	return &TensaiLLM{
		modelPath: modelPath,
		engine:    engine,
		accType:   accType,
		accDetail: accDetail,
	}, nil
}

// ModelPath returns the loaded model path.
func (m *TensaiLLM) ModelPath() string {
	return m.modelPath
}

// Acceleration returns the current active acceleration type and hardware details.
func (m *TensaiLLM) Acceleration() (AccelerationType, string) {
	if m.engine != nil && m.engine.gpu != nil {
		return AccelGPU, m.accDetail
	}
	return m.accType, m.accDetail
}

// Call generates text from a prompt.
func (m *TensaiLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	resp, err := m.GenerateContent(ctx, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, prompt),
	}, options...)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no response choices returned")
	}
	return resp.Choices[0].Content, nil
}

// GenerateContent generates responses for structured chat messages.
func (m *TensaiLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if m.engine == nil {
		err := errors.New("tensai LLM engine is not initialized")
		log.Printf("TensaiLLM: GenerateContent error: %v", err)
		return nil, err
	}

	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	var formatted strings.Builder
	hasSystem := false
	for _, msg := range messages {
		var textBuf strings.Builder
		for _, part := range msg.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				textBuf.WriteString(textPart.Text)
			}
		}
		text := strings.TrimSpace(textBuf.String())
		if text == "" {
			continue
		}
		switch msg.Role {
		case llms.ChatMessageTypeSystem:
			hasSystem = true
			formatted.WriteString("<|im_start|>system\n")
			formatted.WriteString(text)
			formatted.WriteString("<|im_end|>\n")
		case llms.ChatMessageTypeAI:
			formatted.WriteString("<|im_start|>assistant\n")
			formatted.WriteString(text)
			formatted.WriteString("<|im_end|>\n")
		default:
			formatted.WriteString("<|im_start|>user\n")
			formatted.WriteString(text)
			formatted.WriteString("<|im_end|>\n")
		}
	}

	var formattedPrompt string
	if formatted.Len() > 0 {
		if !hasSystem {
			formattedPrompt = "<|im_start|>system\nYou are a helpful natural environment, meteorology, and disaster prevention assistant. Follow the user's instructions and language request carefully.<|im_end|>\n" + formatted.String()
		} else {
			formattedPrompt = formatted.String()
		}
		formattedPrompt += "<|im_start|>assistant\n"
	}

	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	responseContent, err := m.engine.generateFormatted(ctx, formattedPrompt, maxTokens, opts.StreamingFunc)
	if err != nil {
		log.Printf("TensaiLLM: GenerateText error: %v", err)
		return nil, fmt.Errorf("tensai inference error: %w", err)
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: responseContent,
			},
		},
	}, nil
}
