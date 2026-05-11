# Runbook — Suspicious domain queried from internal host

When an internal endpoint queries a domain that is unfamiliar, newly registered, or matches a typosquat pattern, follow this runbook.

## Triage in 5 minutes

1. **Resolve all record types** for the domain: A, AAAA, MX, NS, TXT. Capture the answers along with the recursive resolver path.
2. **Reverse-DNS** the resolved IPs. PTR records that point to consumer ISP space or to known bulletproof-hosting ASNs increase confidence the domain is malicious.
3. **Check the internal threat-intel feed**. A TXT lookup at `<domain>.threats.aether.local` returns a verdict if any feed has scored the domain. A confidence above 0.8 with a malicious category is enough to act.
4. **Check the asset's posture.** If the endpoint is a workstation with a privileged user logged in, escalate severity by one step.

## Containment

- For workstations: block the **domain** at the recursive resolver (NXDOMAIN response) **and** block the resolved IPs at the perimeter. Domain blocks alone leak around DNS-over-HTTPS clients.
- For servers: pause egress at the perimeter and open a forensic image. Do not block at the resolver alone; production services may share infrastructure with adversary-controlled domains.

## Documentation

Every action taken must be filed as an action in the incident's `actions` array. Required fields: kind, target, severity, reason. Containment actions targeting a /16 or larger require a senior analyst's explicit approval.
