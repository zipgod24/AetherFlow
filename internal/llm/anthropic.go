package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Anthropic is an adapter for the Anthropic Messages API.
type Anthropic struct {
	baseURL   string
	apiKey    KeyMaterial
	chatModel string
	hc        *http.Client
}

// NewAnthropic constructs an adapter.
func NewAnthropic(baseURL string, apiKey KeyMaterial, chatModel string) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Anthropic{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		chatModel: chatModel,
		hc:        &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns "anthropic".
func (a *Anthropic) Name() string { return "anthropic" }

// ChatModel returns the configured chat model.
func (a *Anthropic) ChatModel() string { return a.chatModel }

// EmbedModel returns "" — Anthropic does not currently offer first-party
// embeddings. The Reasoner code path tolerates this; embedding always goes
// through the configured Ollama / OpenAI provider.
func (a *Anthropic) EmbedModel() string { return "" }

type anthMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthChatReq struct {
	Model     string        `json:"model"`
	Messages  []anthMessage `json:"messages"`
	System    string        `json:"system,omitempty"`
	MaxTokens int           `json:"max_tokens"`
	Temperature float32     `json:"temperature,omitempty"`
}

type anthChatResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat performs a single completion.
func (a *Anthropic) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = a.chatModel
	}
	// Split out system messages — Anthropic accepts a single top-level system field.
	var system strings.Builder
	msgs := make([]anthMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(m.Content)
			continue
		}
		msgs = append(msgs, anthMessage{Role: m.Role, Content: m.Content})
	}

	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 2048
	}

	body := anthChatReq{
		Model:       model,
		Messages:    msgs,
		System:      system.String(),
		MaxTokens:   maxTok,
		Temperature: req.Temperature,
	}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey.Expose())
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := a.hc.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("anthropic chat: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var parsed anthChatResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic chat decode: %w", err)
	}
	var text strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return ChatResponse{
		Content:      text.String(),
		PromptTokens: parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		FinishReason: parsed.StopReason,
	}, nil
}

// Embed is unsupported on Anthropic.
func (a *Anthropic) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, ErrUnsupported
}
