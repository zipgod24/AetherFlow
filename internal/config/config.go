// Package config loads configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime configuration for any AetherFlow binary.
type Config struct {
	// RabbitMQ
	RabbitMQURL      string
	RabbitMQExchange string
	RabbitMQDLX      string
	RabbitMQPrefetch int

	// Postgres
	PostgresDSN      string
	PostgresMaxConns int

	// LLM
	LLMProvider string

	OllamaBaseURL    string
	OllamaChatModel  string
	OllamaEmbedModel string

	OpenAIBaseURL    string
	OpenAIAPIKey     string
	OpenAIChatModel  string
	OpenAIEmbedModel string

	AnthropicBaseURL   string
	AnthropicAPIKey    string
	AnthropicChatModel string

	// Security
	MasterKey          string
	EventSigningKey    string
	AllowPrivateLLM    bool

	// OTel
	OTLPEndpoint     string
	ServiceNamespace string

	// Gateway
	GatewayHTTPAddr string
	GatewayUIPath   string

	// DNS
	DNSBase     string
	DNSResolver string
}

// Load reads configuration from environment, applying sensible defaults so
// `go run ./cmd/...` works in dev without a full .env.
func Load() Config {
	return Config{
		RabbitMQURL:      env("RABBITMQ_URL", "amqp://aether:aether@localhost:5672/"),
		RabbitMQExchange: env("RABBITMQ_EXCHANGE", "aether.events"),
		RabbitMQDLX:      env("RABBITMQ_DLX", "aether.dlx"),
		RabbitMQPrefetch: envInt("RABBITMQ_PREFETCH", 16),

		PostgresDSN:      env("POSTGRES_DSN", "postgres://aether:aether@localhost:5432/aether?sslmode=disable"),
		PostgresMaxConns: envInt("POSTGRES_MAX_CONNS", 20),

		LLMProvider:      env("AETHER_LLM_PROVIDER", "ollama"),
		OllamaBaseURL:    env("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaChatModel:  env("OLLAMA_CHAT_MODEL", "llama3.1:8b-instruct"),
		OllamaEmbedModel: env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),

		OpenAIBaseURL:    env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIAPIKey:     env("OPENAI_API_KEY", ""),
		OpenAIChatModel:  env("OPENAI_CHAT_MODEL", "gpt-4o-mini"),
		OpenAIEmbedModel: env("OPENAI_EMBED_MODEL", "text-embedding-3-small"),

		AnthropicBaseURL:   env("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		AnthropicAPIKey:    env("ANTHROPIC_API_KEY", ""),
		AnthropicChatModel: env("ANTHROPIC_CHAT_MODEL", "claude-3-5-sonnet-latest"),

		MasterKey:       env("AETHER_MASTER_KEY", ""),
		EventSigningKey: env("AETHER_EVENT_SIGNING_KEY", ""),
		AllowPrivateLLM: envBool("AETHER_ALLOW_PRIVATE_LLM", false),

		OTLPEndpoint:     env("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ServiceNamespace: env("OTEL_SERVICE_NAMESPACE", "aetherflow"),

		GatewayHTTPAddr: env("GATEWAY_HTTP_ADDR", ":8080"),
		GatewayUIPath:   env("GATEWAY_UI_PATH", "./web/ui"),

		DNSBase:     env("AETHER_DNS_BASE", "aether.local"),
		DNSResolver: env("AETHER_DNS_RESOLVER", ""),
	}
}

// HTTPTimeout returns a sensible default for outbound HTTP requests.
func (c Config) HTTPTimeout() time.Duration { return 30 * time.Second }

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// MustHave returns an error if any of the required env keys are empty.
func (c Config) MustHave(keys ...string) error {
	for _, k := range keys {
		if env(k, "") == "" {
			return fmt.Errorf("required env var not set: %s", k)
		}
	}
	return nil
}
