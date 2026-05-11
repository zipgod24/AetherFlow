// Package events defines the versioned event schemas exchanged on the
// aether.events topic exchange.
//
// Every event carries enough metadata to fully reconstruct an incident's
// timeline from the bus alone:
//
//   - IncidentID groups every event in a single workflow.
//   - TraceID is the W3C trace id (also propagated via AMQP headers).
//   - IdempotencyKey allows safe replay.
//   - SchemaVersion is encoded in the routing key (e.g. incident.created.v1)
//     and again in the payload for cross-checks.
package events

import (
	"encoding/json"
	"time"
)

// Routing keys. Keep these stable; bump the version suffix on breaking changes.
const (
	KeyIncidentCreated    = "incident.created.v1"
	KeyContextAssembled   = "context.assembled.v1"
	KeyAnalysisCompleted  = "analysis.completed.v1"
	KeyAnalysisValidated  = "analysis.validated.v1"
	KeyValidationRejected = "validation.rejected.v1"
	KeyActionExecuted     = "action.executed.v1"
)

// Header is the envelope metadata included on every event.
type Header struct {
	EventID        string    `json:"event_id"`
	IncidentID     string    `json:"incident_id"`
	TraceID        string    `json:"trace_id,omitempty"`
	IdempotencyKey string    `json:"idempotency_key"`
	SchemaVersion  string    `json:"schema_version"`
	OccurredAt     time.Time `json:"occurred_at"`
	Producer       string    `json:"producer"`
}

// LLMOverride is set when the originating request supplied user keys via the
// UI; it instructs downstream agents to use this provider for this incident
// only (the cluster default is untouched).
type LLMOverride struct {
	Provider string `json:"provider"`           // "openai" | "anthropic" | "openai_compatible"
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	// APIKey is decrypted by the gateway and re-encrypted with a short-lived
	// per-incident key wrapped in the envelope; consumers decrypt with the
	// AETHER_MASTER_KEY. Field is omitted from logs by the redactor.
	APIKey string `json:"api_key,omitempty"`
}

// IncidentCreated kicks off a workflow.
type IncidentCreated struct {
	Header
	Description string            `json:"description"`
	Source      string            `json:"source"` // "ui" | "cli" | "webhook"
	Severity    string            `json:"severity"`
	Tags        map[string]string `json:"tags,omitempty"`
	LLM         *LLMOverride      `json:"llm,omitempty"`
}

// Evidence is a single retrieved chunk plus retrieval metadata.
type Evidence struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Source     string  `json:"source"`
	Title      string  `json:"title"`
	URL        string  `json:"url,omitempty"`
	Snippet    string  `json:"snippet"`
	DenseScore float32 `json:"dense_score"`
	SparseRank int     `json:"sparse_rank"`
	FusedScore float32 `json:"fused_score"`
}

// DNSObservation captures one DNS-tool execution by the retriever.
type DNSObservation struct {
	Tool    string   `json:"tool"`              // "resolve_a" | "resolve_mx" | "threat_intel_txt"
	Target  string   `json:"target"`
	Records []string `json:"records,omitempty"`
	Verdict string   `json:"verdict,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// ContextAssembled is published by the Retriever.
type ContextAssembled struct {
	Header
	Description     string           `json:"description"`
	Severity        string           `json:"severity"`
	Evidence        []Evidence       `json:"evidence"`
	DNSObservations []DNSObservation `json:"dns_observations,omitempty"`
	Degraded        bool             `json:"degraded,omitempty"` // true if any tool failed
	LLM             *LLMOverride     `json:"llm,omitempty"`
}

// RecommendedAction is what the Reasoner proposes (and the Executor may carry out).
type RecommendedAction struct {
	Kind     string            `json:"kind"`     // "block_ip" | "block_domain" | "page_oncall" | "create_ticket"
	Target   string            `json:"target"`
	Severity string            `json:"severity"`
	Reason   string            `json:"reason"`
	Args     map[string]string `json:"args,omitempty"`
}

// IncidentAnalysis is the strict-schema output of the Reasoner.
type IncidentAnalysis struct {
	Verdict        string              `json:"verdict"` // "malicious" | "suspicious" | "benign" | "unknown"
	Confidence     float32             `json:"confidence"`
	Summary        string              `json:"summary"`
	IOCs           []string            `json:"iocs"`
	Citations      []string            `json:"citations"` // chunk_ids from Evidence
	Actions        []RecommendedAction `json:"actions"`
}

// AnalysisCompleted is published by the Reasoner.
type AnalysisCompleted struct {
	Header
	Analysis IncidentAnalysis `json:"analysis"`
	Evidence []Evidence       `json:"evidence"` // pass-through for grounding check
}

// AnalysisValidated is published by the Validator.
type AnalysisValidated struct {
	Header
	Analysis IncidentAnalysis `json:"analysis"`
	Notes    []string         `json:"notes,omitempty"` // non-blocking warnings
}

// ValidationRejected is published when the Validator rejects an analysis.
type ValidationRejected struct {
	Header
	Reason  string   `json:"reason"`
	Markers []string `json:"markers,omitempty"` // matched injection markers, etc.
}

// ActionExecuted is published by the Executor.
type ActionExecuted struct {
	Header
	Action    RecommendedAction `json:"action"`
	Outcome   string            `json:"outcome"` // "executed" | "skipped" | "failed"
	Detail    string            `json:"detail,omitempty"`
	Receipt   string            `json:"receipt,omitempty"`
	ExecutedAt time.Time        `json:"executed_at"`
}

// Encode marshals a payload to JSON.
func Encode(v any) ([]byte, error) { return json.Marshal(v) }

// Decode unmarshals a payload from JSON.
func Decode[T any](b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}
