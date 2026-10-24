---
title: Git sources and SSRF
---

# Git sources and the SSRF host policy

When you deploy an application from a Git URL, the control plane clones that
URL. Because the URL is operator-supplied, every clone and every
Test-connection dial passes a host policy that blocks internal and cloud-metadata
addresses.

## What the policy does

Every dial passes the SSRF host policy (`internal/deploy/githostpolicy.go`).
Loopback, link-local (including the `169.254.169.254` cloud metadata address),
unspecified, multicast, carrier-grade NAT, benchmarking, NAT64 and 6to4 remotes
are always refused, as are numeric-IP tricks (`2130706433`, `0x7f.0.0.1`),
userinfo and IPv6 zone ids.

RFC1918, ULA and loopback remotes are refused too, unless you allow private
hosts for a self-hosted git server:

```yaml
# gotham.yaml
deploy:
  git_allow_private_hosts: true
```

or `GOTHAM_GIT_ALLOW_PRIVATE_HOSTS=true` in the service environment (`1` and
`true` both count, matching the config file reader). Link-local and the other
always-denied ranges stay denied.

Note the name/IP split: the `localhost` NAME stays denied even with the setting
(names rebind), while an explicit `127.0.0.1` is allowed by it.

The host is resolved twice and a changed answer (DNS rebinding) refuses the
dial; `https` remotes are pinned to the resolved address
(`http.curloptResolve`), and every `http(s)` git invocation refuses redirects
(`http.followRedirects=false`), so a public URL cannot bounce to an internal one
(renamed repositories fail closed — that is the trade-off). Deploy-log errors
carry the classified hint only — never resolved internal addresses. Local paths
and `file://` URLs stay behind `GOTHAM_DEV_CLONE_LOCAL` (development fixtures
only).

## What the policy does not cover

- `ssh` and `git://` remotes are not pinned (git offers no equivalent option):
  their two compared resolutions are the whole mitigation, so a fast-flux answer
  racing that window still reaches the dial.
- An `http(s)` proxy (`HTTP(S)_PROXY` in the service environment) bypasses the
  pin: curl resolves and connects through the proxy, so proxy environments must
  pair this policy with proxy rules that refuse internal targets.
- The pin needs git >= 2.37 (older git ignores `http.curloptResolve`
  silently). The control plane logs a startup warning naming the installed
  version when it predates 2.37; upgrade git to restore the pin.

See [Configuration](/reference/configuration#git-source-policy) for the config
key.
