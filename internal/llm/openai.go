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

// OpenAI is an adapter for the OpenAI Chat Completions + Embeddings APIs.
// Any OpenAI-compatible server (vLLM, LM Studio, etc.) works by setting baseURL.
type OpenAI struct {
	baseURL    string
	apiKey     KeyMaterial
	chatModel  string
	embedModel string
	hc         *http.Client
}

// NewOpenAI constructs an adapter.
func NewOpenAI(baseURL string, apiKey KeyMaterial, chatModel, embedModel string) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		chatModel:  chatModel,
		embedModel: embedModel,
		hc:         &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns "openai".
func (o *OpenAI) Name() string { return "openai" }

// ChatModel returns the configured chat model.
func (o *OpenAI) ChatModel() string { return o.chatModel }

// EmbedModel returns the configured embedding model.
func (o *OpenAI) EmbedModel() string { return o.embedModel }

type openaiChatReq struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Temperature    float32   `json:"temperature,omitempty"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	ResponseFormat any       `json:"response_format,omitempty"`
}

type openaiChatResp struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Chat performs a non-streaming chat completion.
func (o *OpenAI) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.chatModel
	}
	body := openaiChatReq{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.Format != nil && req.Format.Strict {
		body.ResponseFormat = map[string]string{"type": "json_object"}
	}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey.Expose())
	resp, err := o.hc.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("openai chat: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var parsed openaiChatResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("openai chat decode: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("openai chat: empty choices")
	}
	return ChatResponse{
		Content:      parsed.Choices[0].Message.Content,
		PromptTokens: parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		FinishReason: parsed.Choices[0].FinishReason,
	}, nil
}

type openaiEmbedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns one vector per text.
func (o *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if o.embedModel == "" {
		return nil, fmt.Errorf("openai: no embed model configured")
	}
	body := openaiEmbedReq{Model: o.embedModel, Input: texts}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/embeddings", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey.Expose())
	resp, err := o.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai embed: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var parsed openaiEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("openai embed decode: %w", err)
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
