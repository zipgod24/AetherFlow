// Package sse is a tiny server-sent-events fan-out the gateway uses to push
// incident timeline updates to connected browsers.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Event is what subscribers receive.
type Event struct {
	IncidentID string `json:"incident_id"`
	Type       string `json:"type"`
	Payload    any    `json:"payload"`
}

// Hub holds per-incident subscriber sets.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[string]map[chan Event]struct{}{}} }

// Subscribe registers a channel for incidentID (or "*" for all).
func (h *Hub) Subscribe(incidentID string) chan Event {
	ch := make(chan Event, 32)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[incidentID] == nil {
		h.subs[incidentID] = map[chan Event]struct{}{}
	}
	h.subs[incidentID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a channel.
func (h *Hub) Unsubscribe(incidentID string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[incidentID]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.subs, incidentID)
		}
	}
	close(ch)
}

// Publish fans an event out to subscribers of its IncidentID and to "*".
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, key := range []string{e.IncidentID, "*"} {
		for ch := range h.subs[key] {
			select {
			case ch <- e:
			default:
				// Slow subscriber — drop the event rather than block the bus.
			}
		}
	}
}

// ServeHTTP renders an SSE stream for ?incident=<id|*> until the client disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	incident := r.URL.Query().Get("incident")
	if incident == "" {
		incident = "*"
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := h.Subscribe(incident)
	defer h.Unsubscribe(incident, ch)

	ctx := r.Context()
	// Initial comment so curl shows something immediately.
	fmt.Fprintf(w, ": connected to %s\n\n", incident)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-context.Background().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\n", e.Type)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}
	}
}
