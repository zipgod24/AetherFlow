# Runbook — Outbound traffic to known Tor exit nodes

Tor egress from an internal host is rarely legitimate on a corporate network. The trade-off is between adversary tradecraft (Tor is heavily used for C2 and exfiltration) and a small set of legitimate use cases (red-team operations, certain privacy-research workflows).

## Detection

- The threat-intel zone (`<ip>.threats.aether.local`) returns a TXT record with `cat=tor_exit` for known exit IPs.
- Outbound connections to a Tor entry-guard ASN immediately followed by an HTTPS handshake to a non-CDN IP within seconds.

## Triage

1. Check whether the user has a sanctioned Tor exemption (table `iam.tor_exempt_users`).
2. If not exempt, treat the connection as a probable indicator of compromise and proceed to containment.

## Containment

- Block the specific Tor exit IP at the perimeter. **Do not** block the user's egress unless you have evidence of further compromise.
- Page the on-call. Priority depends on asset class — production server traffic to a Tor exit is a P1; workstation traffic is a P2 unless privileged data was accessed during the window.
- Open a ticket capturing the full TLS metadata of the offending connection.
