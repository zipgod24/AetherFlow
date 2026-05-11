# T1071 — Application Layer Protocol

Adversaries may communicate using application layer protocols associated with web traffic to avoid detection and network filtering by blending in with existing traffic. Commands to the remote system, and often the results of those commands, will be embedded within the protocol traffic between the client and server.

Common protocols include HTTP, HTTPS, and DNS. The same techniques used by legitimate applications make detection difficult: TLS encryption hides payload, and high-volume web traffic provides ample noise.

## Indicators

- Long-lived DNS queries to a single unfamiliar domain at regular intervals (beaconing).
- TXT records with high-entropy strings — often exfil data or C2 commands encoded in base64.
- Newly-registered domains that look like typosquats of well-known brands (e.g. `paypa1-secure-login.com`, `g00gle-auth.com`).
- TLS certificates that match patterns from previously catalogued malware families.

## Recommended detection

- Baseline DNS query volume per (host, domain) pair; alert on periodicity that matches typical beacon intervals (60s, 300s, 3600s).
- Score domains by registration age (anything < 7 days at a high asset-value target is suspicious by default).
- Compare typosquat candidates against your protected-brand list using edit distance and homoglyph-aware matching.

## Recommended response

- Sinkhole the offending domain at the recursive resolver.
- Block egress to resolved IPs at the perimeter firewall.
- Isolate the affected endpoint for IR triage.
