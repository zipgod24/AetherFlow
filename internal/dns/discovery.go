// Package dns wraps the standard-library resolver to provide
//
//   - SRV-based service discovery (works in docker-compose with CoreDNS and
//     in Kubernetes Headless Services with zero code changes), and
//   - DNS-as-a-tool helpers used by the retriever (A/AAAA/MX/NS/TXT,
//     plus an opinionated "threat-intel TXT" verdict lookup).
package dns

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"
)

// Resolver is the configurable wrapper used everywhere in AetherFlow.
type Resolver struct {
	res *net.Resolver
}

// NewResolver returns a Resolver. If resolverAddr is non-empty (e.g. "1.1.1.1:53")
// it forces the Go resolver to use that nameserver instead of the host's.
func NewResolver(resolverAddr string) *Resolver {
	r := &net.Resolver{PreferGo: true}
	if resolverAddr != "" {
		r.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, resolverAddr)
		}
	}
	return &Resolver{res: r}
}

// Endpoint is a discovered network address.
type Endpoint struct {
	Host     string
	Port     int
	Priority uint16
	Weight   uint16
}

// String returns host:port.
func (e Endpoint) String() string { return fmt.Sprintf("%s:%d", e.Host, e.Port) }

// DiscoverSRV resolves _service._proto.base and returns endpoints sorted by
// SRV priority (lower first), then weight (higher first).
func (r *Resolver) DiscoverSRV(ctx context.Context, service, proto, base string) ([]Endpoint, error) {
	_, addrs, err := r.res.LookupSRV(ctx, service, proto, base)
	if err != nil {
		return nil, fmt.Errorf("SRV %s.%s.%s: %w", service, proto, base, err)
	}
	out := make([]Endpoint, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, Endpoint{
			Host:     trimTrailingDot(a.Target),
			Port:     int(a.Port),
			Priority: a.Priority,
			Weight:   a.Weight,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Weight > out[j].Weight
	})
	return out, nil
}

// LookupA returns A/AAAA records for host.
func (r *Resolver) LookupA(ctx context.Context, host string) ([]string, error) {
	ips, err := r.res.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	return ips, nil
}

// LookupMX returns MX records (preference + host) as strings.
func (r *Resolver) LookupMX(ctx context.Context, host string) ([]string, error) {
	mxs, err := r.res.LookupMX(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(mxs))
	for _, m := range mxs {
		out = append(out, fmt.Sprintf("%d %s", m.Pref, trimTrailingDot(m.Host)))
	}
	return out, nil
}

// LookupNS returns NS records.
func (r *Resolver) LookupNS(ctx context.Context, host string) ([]string, error) {
	ns, err := r.res.LookupNS(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, trimTrailingDot(n.Host))
	}
	return out, nil
}

// LookupTXT returns TXT records.
func (r *Resolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	return r.res.LookupTXT(ctx, host)
}

func trimTrailingDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
