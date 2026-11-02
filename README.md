# Gotham — Self-Hosted PaaS

Gotham is a self-hosted Platform-as-a-Service (PaaS), built with **Go** (backend + agent) and **Vue 3** (frontend). One `gotham` binary runs the control plane; one `gotham-agent` binary runs on each managed node.

Design priorities (in order):

1. **Compatibility** — wide OS / Docker / database support matrix, graceful degradation on older nodes.
2. **Extensibility** — modular monolith with clean boundaries, versioned gRPC contracts, template-driven services.
3. **Self-updating** — signed automatic updates for both the control plane and node agents, with rollback.
4. **Lean footprint** — comfortable on small servers (≥ 1 GB RAM, 1 vCPU), but not at the cost of the three priorities above.
