# T1566 — Phishing

Adversaries may send phishing messages to gain access to victim systems. Phishing can be targeted (spearphishing) or non-targeted, and is conducted via email, SMS, messaging apps, or any other communication channel. Phishing for credentials often relies on **typosquat domains** that imitate the brand the user expects to authenticate with.

## Typosquat indicators

- Single-character substitution: `paypa1` for `paypal`, `g00gle` for `google`, `arnaz0n` for `amazon`.
- Homoglyph substitution: `аpple.com` (Cyrillic а) for `apple.com`.
- Subdomain confusion: `paypal.secure-login.com` where `secure-login.com` is the attacker-owned apex.
- Brand keyword with a security-themed suffix: `-secure`, `-auth`, `-verify`, `-support`.

A workstation issuing DNS queries to a typosquat that resolves to an IP outside the legitimate ASN of the brand is one of the strongest single signals for active credential phishing.

## Recommended response

- Block the typosquat domain at the recursive resolver.
- Page the on-call analyst — typosquat-based phishing has a short golden hour between user click and credential submission.
- Open a phishing ticket with the URL, resolved IPs, screenshot of the phishing page, and the affected endpoint for follow-up.
