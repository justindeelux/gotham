---
title: Domain and HTTPS
---

# Domain and HTTPS

You can use Gotham at `http://<server-ip>:8000` right after the install. This
page turns that into your own domain with a trusted certificate, either during
the install or later in the panel.

## What you need

- A domain (or subdomain) whose A/AAAA records point at the control-plane
  server.
- Inbound ports 80 and 443 open on that server.
- The `localhost` node of the control plane running Traefik. The installer
  sets this up by default.
- If the domain is behind Cloudflare, set the record to **DNS only** (proxy
  off) for the normal HTTP-01 flow, and remove stale AAAA records when the
  server cannot serve IPv6.

## Set the domain during the install

The installer asks `Panel domain (optional, Enter skips)`. Type the domain, or
pass it without a prompt:

Add `-s -- --domain panel.example.com` after `sh` in the install command (see
[Install](/guide/install)).

Before applying the domain, the installer waits up to **60 seconds** for the
localhost agent to become ready. A timeout leaves the host HTTP address
available. It then:

1. sets the instance `control_plane_url` to `https://panel.example.com`;
2. restarts the service so the panel answers on the new address;
3. prints the HTTP address to open.

Open `http://panel.example.com` and confirm the domain in **Settings**
within **2 minutes** to start HTTPS issuance. An unconfirmed trial reverts
automatically. Invalid domains and DNS failures print a warning and leave the
host HTTP address available; you can set the domain later in the panel.

## Set the domain later

1. Open the panel on `http://<server-ip>:8000`.
2. In **Settings**, set the instance `control_plane_url` to
   `https://panel.example.com` and save.
3. Open the proposed host over HTTP and confirm the change before the trial
   expires.

Saving starts a confirmation trial and creates a panel-owned certificate
intent; no application is required. Both the proposed and previous hosts stay
routed during the trial, and a revert restores the previous host if issuance
fails.

The HTTPS router starts ACME issuance while HTTP continues forwarding to the
panel. HTTP redirects to HTTPS only after the local agent reports an unexpired
certificate; readiness is refreshed every 30 seconds. Pending or failed
issuance keeps HTTP available.

## HTTP-01 and DNS-01

HTTP-01 is the default: the domain's A/AAAA records must point directly at this
server, and Cloudflare records must be **DNS only** (proxy disabled).

To keep Cloudflare's proxy enabled, configure a DNS provider and select DNS-01
for the panel intent through the owner-only
`PATCH /api/v1/proxy/panel-certificates/{id}` API, using the existing
`challenge` and `dns_provider_id` fields. DNS-01 needs a token allowed to
update the configured zone; use Full (strict) once the origin certificate is
issued.

## What the panel and API report

The routers API reports the proposed host during a trial and adds
`certificate.status` (`issued`, `pending`, or `failed`) and
`certificate.not_after` for panel rows. The owner-only
`GET /api/v1/proxy/panel-certificates` API lists panel intents with the existing
storage `status` plus additive `panel_state` and `owner_kind` fields.
`pending` means no usable certificate has been observed yet; `failed` includes
unusable configuration or unavailable storage, since the agent does not report
ACME order errors. Application certificate APIs follow the application's project
access.

If HTTPS or DNS still fails, see
[Troubleshooting](/guide/troubleshooting#panel-https-or-dns-fails).
