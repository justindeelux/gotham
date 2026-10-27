---
title: Reference
---

# Reference

Lookup material for owners who already know what they want to change: the
commands on the control plane and node hosts, every configuration key and
environment variable, and the API-token model.

- [CLI](/reference/cli) — `gotham`, `gotham-agent` and the release `signer`:
  commands, flags, defaults and examples for the two service binaries.
- [Configuration](/reference/configuration) — the `gotham.yaml` keys, their
  `GOTHAM_*` environment overrides, the agent's environment, and the nine
  `FEATURE_*` switches.
- [API tokens](/reference/api-tokens) — `gt1_` tokens, the `read`, `deploy` and
  `admin` scopes, and how to create, rotate and revoke them.

These pages describe the shipped behaviour, not a roadmap. Where a page and the
binary disagree, the binary is right: `gotham help`, `gotham admin help`,
`gotham ca help`, `gotham update help`, `gotham-agent help` and `signer help`
print the command surface of the build in front of you.

## Where to start instead

- Installing the control plane and enrolling the first node:
  [Install](/guide/install).
- Updating and rolling back: [Updates](/guide/updates).
- The permission model behind tokens, the owner and members:
  [Security](/operating/security).
