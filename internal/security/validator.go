// Package security houses the adversarial-defense primitives used by the
// Validator agent and the gateway's input sanitizer.
//
// The job here is not to be exhaustive — adversarial input is a research
// arms race — but to ship layered, auditable checks that catch the obvious
// failure modes and that can be extended without touching agent code.
package security

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Marker is one detected suspicious pattern in input or output.
type Marker struct {
	Kind     string // "injection" | "encoded_payload" | "role_flip" | "structural"
	Match    string
	Severity string // "low" | "medium" | "high"
}

// Result is what InjectionScan / OutputScan return.
type Result struct {
	Markers []Marker
}

// HighestSeverity returns "", "low", "medium", or "high".
func (r Result) HighestSeverity() string {
	rank := map[string]int{"low": 1, "medium": 2, "high": 3}
	best := ""
	for _, m := range r.Markers {
		if rank[m.Severity] > rank[best] {
			best = m.Severity
		}
	}
	return best
}

var (
	// Classic prompt-injection lexicon. Case-insensitive, anchored only loosely.
	rxIgnorePrev = regexp.MustCompile(`(?i)\bignore (?:all|the|any)?\s*(?:previous|prior|above)\s+(?:instructions|prompts|messages)\b`)
	rxOverride   = regexp.MustCompile(`(?i)\b(?:disregard|forget|override)\s+(?:all|the|any|previous)?\s*(?:instructions|prompts)\b`)
	rxRoleFlip   = regexp.MustCompile(`(?i)\b(you are now|act as|pretend to be)\s+(?:an?\s+)?(?:admin|root|developer|jailbroken|dan|unrestricted)`)
	rxSysImpers  = regexp.MustCompile(`(?i)^\s*(?:###\s*)?system\s*:`)
	rxToolHijack = regexp.MustCompile(`(?i)\b(?:execute|run|invoke)\b.*\b(?:rm\s+-rf|/etc/passwd|secret_key|api_key)\b`)
	rxEncodedB64 = regexp.MustCompile(`(?:[A-Za-z0-9+/]{60,}={0,2})`)
	rxHexBlob    = regexp.MustCompile(`(?:0x)?[0-9A-Fa-f]{80,}`)
)

// InjectionScan checks free-form text for injection markers. Run this on
// incident descriptions, retrieved chunks (as a hygiene measure), and any
// other untrusted input.
func InjectionScan(text string) Result {
	var r Result
	add := func(kind, match, sev string) {
		r.Markers = append(r.Markers, Marker{Kind: kind, Match: trim(match, 80), Severity: sev})
	}
	if m := rxIgnorePrev.FindString(text); m != "" {
		add("injection", m, "high")
	}
	if m := rxOverride.FindString(text); m != "" {
		add("injection", m, "high")
	}
	if m := rxRoleFlip.FindString(text); m != "" {
		add("role_flip", m, "high")
	}
	if m := rxSysImpers.FindString(text); m != "" {
		add("injection", m, "medium")
	}
	if m := rxToolHijack.FindString(text); m != "" {
		add("injection", m, "high")
	}
	if m := rxEncodedB64.FindString(text); m != "" {
		add("encoded_payload", m, "medium")
	}
	if m := rxHexBlob.FindString(text); m != "" {
		add("encoded_payload", m, "low")
	}

	// Heuristic: massive non-printable density.
	if utf8.RuneCountInString(text) > 0 {
		bad := 0
		for _, r := range text {
			if !isPrintable(r) {
				bad++
			}
		}
		if bad*4 > utf8.RuneCountInString(text) {
			add("structural", "non-printable density", "medium")
		}
	}
	return r
}

func isPrintable(r rune) bool {
	switch {
	case r == '\n' || r == '\r' || r == '\t' || r == ' ':
		return true
	case r < 0x20:
		return false
	case r == 0x7f:
		return false
	}
	return true
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// CheckCitations verifies every cited chunk ID was present in the retrieved
// evidence set. Returns the missing IDs.
func CheckCitations(citations []string, retrieved []string) []string {
	set := map[string]struct{}{}
	for _, id := range retrieved {
		set[id] = struct{}{}
	}
	var missing []string
	for _, c := range citations {
		if c == "" {
			continue
		}
		if _, ok := set[c]; !ok {
			missing = append(missing, c)
		}
	}
	return missing
}

// ToolSafety enforces bounds on recommended actions.
type ToolSafety struct {
	MaxPagePriority int      // 1=highest, 5=lowest; reject anything stricter
	MaxBlockPrefix  int      // CIDR prefix length floor for block_ip (e.g. 24 forbids /16)
	AllowedKinds    []string // whitelist of action kinds
}

// DefaultToolSafety returns reasonable production-ish bounds.
func DefaultToolSafety() ToolSafety {
	return ToolSafety{
		MaxPagePriority: 2,
		MaxBlockPrefix:  24,
		AllowedKinds:    []string{"block_ip", "block_domain", "page_oncall", "create_ticket"},
	}
}

// CheckAction returns "" if the action is in bounds, otherwise a reason.
func (t ToolSafety) CheckAction(kind, target string, args map[string]string) string {
	allowed := false
	for _, k := range t.AllowedKinds {
		if k == kind {
			allowed = true
			break
		}
	}
	if !allowed {
		return "action kind not in whitelist: " + kind
	}
	switch kind {
	case "block_ip":
		if !strings.Contains(target, "/") {
			return "" // single IP is fine
		}
		// crude prefix parse
		_, mask, ok := strings.Cut(target, "/")
		if !ok {
			return "malformed CIDR: " + target
		}
		prefix := 0
		for _, r := range mask {
			if r < '0' || r > '9' {
				return "malformed CIDR mask"
			}
			prefix = prefix*10 + int(r-'0')
		}
		if prefix < t.MaxBlockPrefix {
			return "prefix too wide (max /" + mask + ")"
		}
	case "page_oncall":
		if p, ok := args["priority"]; ok {
			pi := 0
			for _, r := range p {
				if r < '0' || r > '9' {
					return "non-numeric priority"
				}
				pi = pi*10 + int(r-'0')
			}
			if pi < t.MaxPagePriority {
				return "page priority too strict"
			}
		}
	}
	return ""
}
