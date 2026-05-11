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

// Ollama is an adapter for a local or remote Ollama server.
//
// API reference: https://github.com/ollama/ollama/blob/main/docs/api.md
type Ollama struct {
	baseURL    string
	chatModel  string
	embedModel string
	hc         *http.Client
}

// NewOllama constructs an adapter pointing at baseURL.
func NewOllama(baseURL, chatModel, embedModel string) *Ollama {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Ollama{
		baseURL:    strings.TrimRight(baseURL, "/"),
		chatModel:  chatModel,
		embedModel: embedModel,
		hc:         &http.Client{Timeout: 5 * time.Minute},
	}
}

// Name returns "ollama".
func (o *Ollama) Name() string { return "ollama" }

// ChatModel returns the configured chat model name.
func (o *Ollama) ChatModel() string { return o.chatModel }

// EmbedModel returns the configured embedding model name.
func (o *Ollama) EmbedModel() string { return o.embedModel }

type ollamaChatReq struct {
	Model    string               `json:"model"`
	Messages []Message            `json:"messages"`
	Stream   bool                 `json:"stream"`
	Format   string               `json:"format,omitempty"`
	Options  map[string]any       `json:"options,omitempty"`
}

type ollamaChatResp struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool `json:"done"`
	PromptEvalCount int  `json:"prompt_eval_count"`
	EvalCount       int  `json:"eval_count"`
}

// Chat performs a single non-streaming completion.
func (o *Ollama) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.chatModel
	}
	body := ollamaChatReq{
		Model:    model,
		Messages: req.Messages,
		Stream:   false,
		Options: map[string]any{
			"temperature": req.Temperature,
		},
	}
	if req.Format != nil && req.Format.Strict {
		body.Format = "json"
	}
	if req.MaxTokens > 0 {
		body.Options["num_predict"] = req.MaxTokens
	}

	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.hc.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("ollama chat: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var parsed ollamaChatResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat decode: %w", err)
	}
	return ChatResponse{
		Content:      parsed.Message.Content,
		PromptTokens: parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
		FinishReason: "stop",
	}, nil
}

type ollamaEmbedReq struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type ollamaEmbedResp struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed returns one vector per input string.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if o.embedModel == "" {
		return nil, fmt.Errorf("ollama: no embed model configured")
	}
	body := ollamaEmbedReq{Model: o.embedModel, Input: texts}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var parsed ollamaEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	return parsed.Embeddings, nil
}
