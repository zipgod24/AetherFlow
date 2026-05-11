package dns

import (
	"context"
	"strings"
)

// ThreatIntel issues a TXT lookup against a verdict zone of the form
//
//	<sha1-ish-of-target>.threats.aether.local
//
// or a direct query like
//
//	example.com.threats.aether.local
//
// and parses the first TXT record into a Verdict struct. This is a deliberate
// reuse of DNS as a poor-man's signed key-value lookup — the same pattern
// used by SBL/XBL DNSBLs for spam scoring.
type ThreatIntel struct {
	res *Resolver
	// Zone is the suffix appended to the queried target.
	Zone string
}

// Verdict captures the TXT response.
type Verdict struct {
	Found      bool
	Category   string  // e.g. "phishing", "c2", "tor_exit", "benign"
	Confidence float32 // 0..1
	Source     string
	Raw        string
}

// NewThreatIntel constructs a lookup helper.
func NewThreatIntel(res *Resolver, zone string) *ThreatIntel {
	if zone == "" {
		zone = "threats.aether.local"
	}
	return &ThreatIntel{res: res, Zone: zone}
}

// Lookup performs a TXT query and parses the first record.
//
// Expected TXT format (semicolon-delimited):
//
//	cat=phishing;conf=0.92;src=internal-feed
func (t *ThreatIntel) Lookup(ctx context.Context, target string) (Verdict, error) {
	q := target + "." + t.Zone
	txts, err := t.res.LookupTXT(ctx, q)
	if err != nil || len(txts) == 0 {
		// NXDOMAIN is treated as "no verdict", not an error.
		return Verdict{Found: false}, nil
	}
	return parseVerdict(txts[0]), nil
}

func parseVerdict(s string) Verdict {
	v := Verdict{Found: true, Raw: s}
	for _, kv := range strings.Split(s, ";") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		k, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "cat", "category":
			v.Category = strings.TrimSpace(val)
		case "conf", "confidence":
			v.Confidence = parseFloat(val)
		case "src", "source":
			v.Source = strings.TrimSpace(val)
		}
	}
	return v
}

func parseFloat(s string) float32 {
	s = strings.TrimSpace(s)
	var f float32
	var dot bool
	var frac float32 = 1
	for _, r := range s {
		switch {
		case r == '.':
			dot = true
		case r >= '0' && r <= '9':
			d := float32(r - '0')
			if dot {
				frac /= 10
				f += d * frac
			} else {
				f = f*10 + d
			}
		default:
			return f
		}
	}
	return f
}
