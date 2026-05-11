// Package llm provides a thin, provider-agnostic interface for chat completion
// and embedding requests, plus concrete adapters for Ollama, OpenAI, Anthropic,
// and any OpenAI-compatible base URL.
//
// The Reasoner does not import a specific adapter; it asks the Registry for
// a Provider by name. Per-incident overrides (BYO keys submitted from the UI)
// are materialized through Registry.WithOverride.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Role labels for chat turns.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn in a chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat asks the provider for structured output.
type ResponseFormat struct {
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	Strict     bool            `json:"strict,omitempty"`
}

// ChatRequest is the common surface across providers.
type ChatRequest struct {
	Model       string
	Messages    []Message
	Temperature float32
	MaxTokens   int
	Format      *ResponseFormat
}

// ChatResponse carries the assistant's reply plus token counts.
type ChatResponse struct {
	Content       string
	PromptTokens  int
	OutputTokens  int
	FinishReason  string
}

// Provider abstracts a chat + embedding backend.
type Provider interface {
	Name() string
	ChatModel() string
	EmbedModel() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// ErrUnsupported is returned when a provider lacks a capability.
var ErrUnsupported = errors.New("operation unsupported by this provider")

// KeyMaterial wraps secret strings so they can never accidentally be logged.
type KeyMaterial struct{ v string }

// NewKey wraps a key.
func NewKey(s string) KeyMaterial { return KeyMaterial{v: s} }

// String redacts the value.
func (k KeyMaterial) String() string { return "<redacted>" }

// MarshalJSON redacts the value.
func (k KeyMaterial) MarshalJSON() ([]byte, error) { return []byte(`"<redacted>"`), nil }

// Expose returns the underlying value. Use only inside adapter code right
// before forwarding to the upstream provider.
func (k KeyMaterial) Expose() string { return k.v }

// Override is what the gateway materializes from a UI-supplied BYO config.
type Override struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   KeyMaterial
}

// Registry holds the default providers and constructs ad-hoc providers for overrides.
type Registry struct {
	defaultProv Provider
	byName      map[string]Provider
}

// NewRegistry returns a registry with the given default plus a map of named providers.
func NewRegistry(def Provider, named map[string]Provider) *Registry {
	if named == nil {
		named = map[string]Provider{}
	}
	if def != nil {
		named[def.Name()] = def
	}
	return &Registry{defaultProv: def, byName: named}
}

// Default returns the cluster's default provider.
func (r *Registry) Default() Provider { return r.defaultProv }

// ByName returns a provider by name. Returns error if not found.
func (r *Registry) ByName(name string) (Provider, error) {
	p, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p, nil
}

// WithOverride returns a one-shot provider built from o. Caller-supplied keys
// never enter the registry's persistent map; this is per-request only.
func (r *Registry) WithOverride(o Override) (Provider, error) {
	switch o.Provider {
	case "openai", "openai_compatible":
		return NewOpenAI(o.BaseURL, o.APIKey, o.Model, ""), nil
	case "anthropic":
		return NewAnthropic(o.BaseURL, o.APIKey, o.Model), nil
	case "ollama":
		return NewOllama(o.BaseURL, o.Model, ""), nil
	default:
		return nil, fmt.Errorf("unknown override provider: %s", o.Provider)
	}
}
