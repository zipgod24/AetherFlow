package events

import (
	"testing"
	"time"
)

func TestRoundTripIncidentCreated(t *testing.T) {
	ev := IncidentCreated{
		Header: Header{
			EventID:        "e1",
			IncidentID:     "i1",
			IdempotencyKey: "i1",
			SchemaVersion:  "v1",
			OccurredAt:     time.Now().UTC().Truncate(time.Second),
			Producer:       "test",
		},
		Description: "endpoint X talking to suspicious.example.com",
		Source:      "test",
		Severity:    "high",
	}
	b, err := Encode(ev)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode[IncidentCreated](b)
	if err != nil {
		t.Fatal(err)
	}
	if back.IncidentID != ev.IncidentID || back.Description != ev.Description {
		t.Errorf("round trip mismatch: %+v vs %+v", back, ev)
	}
}
