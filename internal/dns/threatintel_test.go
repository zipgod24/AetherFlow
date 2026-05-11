package dns

import "testing"

func TestParseVerdict(t *testing.T) {
	v := parseVerdict("cat=phishing;conf=0.94;src=aether-demo-feed")
	if !v.Found {
		t.Fatal("expected found")
	}
	if v.Category != "phishing" {
		t.Errorf("category=%q", v.Category)
	}
	if v.Confidence < 0.93 || v.Confidence > 0.95 {
		t.Errorf("confidence=%v", v.Confidence)
	}
	if v.Source != "aether-demo-feed" {
		t.Errorf("source=%q", v.Source)
	}
}

func TestParseVerdictTolerantOfWhitespace(t *testing.T) {
	v := parseVerdict("  category = c2 ; confidence = 0.5 ; source = X  ")
	if v.Category != "c2" || v.Source != "X" {
		t.Errorf("verdict = %+v", v)
	}
	if v.Confidence < 0.49 || v.Confidence > 0.51 {
		t.Errorf("conf=%v", v.Confidence)
	}
}
