import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";

/**
 * TextParams interpolates one curated display string (e.g. `{name}`).
 * Wire values are never keys: they travel as parameter values only.
 */
export type TextParams = Record<string, string | number>;

/**
 * proxyText resolves one domains namespaced key in the current locale at
 * invocation time, so a language switch refreshes every caller on its next
 * render. When the feature catalog is not registered (a unit harness
 * importing this module directly) it falls back to the English literal, so
 * existing behavior assertions keep passing. Raw API text never passes
 * through here; see describeProxyError.
 */
export function proxyText(
  key: string,
  fallback: string,
  params?: TextParams,
): string {
  const composer = i18n.global;
  if (composer.te(key)) {
    return String(composer.t(key, params ?? {}));
  }
  let out = fallback;
  for (const [name, value] of Object.entries(params ?? {})) {
    out = out.replaceAll(`{${name}}`, String(value));
  }
  return out;
}

/**
 * Typed client for the proxy SSL routes served by `internal/proxy`
 * (see ssl_routes.go for the contract):
 *
 *   POST   /proxy/dns-providers
 *   GET    /proxy/dns-providers
 *   GET    /proxy/dns-providers/{id}
 *   PATCH  /proxy/dns-providers/{id}
 *   DELETE /proxy/dns-providers/{id}
 *   POST   /proxy/certificates
 *   GET    /proxy/certificates
 *   GET    /proxy/certificates/{id}
 *   PATCH  /proxy/certificates/{id}
 *   DELETE /proxy/certificates/{id}
 *   POST   /proxy/redirects
 *   GET    /proxy/redirects
 *   GET    /proxy/redirects/{id}
 *   PATCH  /proxy/redirects/{id}
 *   DELETE /proxy/redirects/{id}
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * Credentials are write-only: the API never returns a sealed credential, only
 * `credentials_set`. The SPA may set or rotate one and must never render,
 * log or persist it. Certificate responses carry the node-observed `status`
 * and, when a certificate is present, its `not_after` expiry; a missing status
 * means the control plane has no status service configured and must be
 * rendered as not reported.
 */

/** DNS provider types accepted by the control plane (see ssl.go allowlist). */
export type DNSProviderName = "cloudflare" | "digitalocean";

/** ACME challenge mode (see ssl.go). */
export type ChallengeMode = "http-01" | "dns-01";

/** One DNS provider as returned by the API (credential never included). */
export interface DNSProvider {
  id: string;
  provider: DNSProviderName;
  name: string;
  zones: string[];
  enabled: boolean;
  credentials_set: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * Body of POST /proxy/dns-providers. `credential` is the plaintext API token:
 * it is sealed server-side and never returned again.
 */
export interface CreateDNSProviderInput {
  provider: DNSProviderName;
  name?: string;
  zones: string[];
  credential: string;
  enabled?: boolean;
}

/**
 * Body of PATCH /proxy/dns-providers/{id}. Omitted fields stay unchanged; an
 * omitted `credential` keeps the stored one, a present one rotates it.
 */
export interface UpdateDNSProviderInput {
  provider?: DNSProviderName;
  name?: string;
  zones?: string[];
  credential?: string;
  enabled?: boolean;
}

/**
 * Node-observed state of one certificate intent (see certificate_status.go):
 *
 *   present — the node's ACME storage holds a certificate covering the domain
 *   absent  — the storage was read and holds no such certificate
 *   unknown — the node or its storage could not be read; no claim possible
 *
 * The field is omitted when the control plane has no status service, which
 * the UI renders as "not reported" rather than inventing a value.
 */
export type CertificateStatus = "present" | "absent" | "unknown";

/** One certificate configuration as returned by the API. */
export interface Certificate {
  id: string;
  application_id: string;
  /** Recorded host: the primary domain by default, or one attached domain. */
  domain: string;
  enabled: boolean;
  challenge: ChallengeMode;
  /** Empty for http-01. */
  dns_provider_id: string;
  wildcard: boolean;
  /** Observed on read; omitted when no status service is configured. */
  status?: CertificateStatus;
  /** Covering certificate expiry; only present when status is "present". */
  not_after?: string;
  created_at: string;
  updated_at: string;
}

/**
 * Body of POST /proxy/certificates. The recorded domain defaults to the
 * application's primary domain; `domain` selects one of its other attached
 * domains instead (JUS-89).
 */
export interface CreateCertificateInput {
  application_id: string;
  domain?: string;
  enabled?: boolean;
  challenge?: ChallengeMode;
  dns_provider_id?: string;
  wildcard?: boolean;
}

/**
 * Body of PATCH /proxy/certificates/{id}. Omitted fields stay unchanged;
 * `domain` re-targets the intent onto another attached host.
 */
export interface UpdateCertificateInput {
  domain?: string;
  enabled?: boolean;
  challenge?: ChallengeMode;
  dns_provider_id?: string;
  wildcard?: boolean;
}

/**
 * Form state of the certificate editor. The domain is deliberately absent:
 * it is read from the selected application and shown read-only.
 */
export interface CertificateDraft {
  application_id: string;
  challenge: ChallengeMode;
  dns_provider_id: string;
  wildcard: boolean;
  enabled: boolean;
}

/**
 * Redirect code intent accepted by the API (see redirects.go): 301 permanent
 * or 302 temporary. Traefik special-cases only GET, so GET answers the stored
 * code while HEAD and every other method answer 308/307 to keep the method.
 */
export type RedirectCode = 301 | 302;

/** One domain→domain redirect rule as returned by the API. */
export interface DomainRedirect {
  id: string;
  application_id: string;
  source_domain: string;
  target_domain: string;
  code: RedirectCode;
  preserve_path: boolean;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

/** Body of POST /proxy/redirects. code defaults to 301 server-side. */
export interface CreateRedirectInput {
  application_id: string;
  source_domain: string;
  target_domain: string;
  code?: RedirectCode;
  preserve_path?: boolean;
  enabled?: boolean;
}

/** Body of PATCH /proxy/redirects/{id}. Omitted fields stay unchanged. */
export interface UpdateRedirectInput {
  source_domain?: string;
  target_domain?: string;
  code?: RedirectCode;
  preserve_path?: boolean;
  enabled?: boolean;
}

/** Wire envelopes (see ssl_routes.go). */
interface DNSProviderEnvelope {
  provider: DNSProvider;
}

interface DNSProviderListEnvelope {
  providers: DNSProvider[];
}

interface CertificateEnvelope {
  certificate: Certificate;
}

interface CertificateListEnvelope {
  certificates: Certificate[];
}

interface RedirectEnvelope {
  redirect: DomainRedirect;
}

interface RedirectListEnvelope {
  redirects: DomainRedirect[];
}

/** listDNSProviders returns every configured provider, newest first. */
export async function listDNSProviders(): Promise<DNSProvider[]> {
  const response = await http.get<DNSProviderListEnvelope>("/proxy/dns-providers");
  return response.data.providers ?? [];
}

/** createDNSProvider stores one sealed provider row (POST → 201). */
export async function createDNSProvider(
  input: CreateDNSProviderInput,
): Promise<DNSProvider> {
  const response = await http.post<DNSProviderEnvelope>(
    "/proxy/dns-providers",
    input,
  );
  return response.data.provider;
}

/** updateDNSProvider applies a partial update (PATCH → 200). */
export async function updateDNSProvider(
  id: string,
  input: UpdateDNSProviderInput,
): Promise<DNSProvider> {
  const response = await http.patch<DNSProviderEnvelope>(
    `/proxy/dns-providers/${id}`,
    input,
  );
  return response.data.provider;
}

/** deleteDNSProvider removes one provider (DELETE → 204). */
export async function deleteDNSProvider(id: string): Promise<void> {
  await http.delete(`/proxy/dns-providers/${id}`);
}

/** listCertificates returns every certificate configuration, newest first. */
export async function listCertificates(): Promise<Certificate[]> {
  const response = await http.get<CertificateListEnvelope>("/proxy/certificates");
  return response.data.certificates ?? [];
}

/** createCertificate stores one certificate configuration (POST → 201). */
export async function createCertificate(
  input: CreateCertificateInput,
): Promise<Certificate> {
  const response = await http.post<CertificateEnvelope>(
    "/proxy/certificates",
    input,
  );
  return response.data.certificate;
}

/** updateCertificate applies a partial update (PATCH → 200). */
export async function updateCertificate(
  id: string,
  input: UpdateCertificateInput,
): Promise<Certificate> {
  const response = await http.patch<CertificateEnvelope>(
    `/proxy/certificates/${id}`,
    input,
  );
  return response.data.certificate;
}

/** deleteCertificate removes one certificate configuration (DELETE → 204). */
export async function deleteCertificate(id: string): Promise<void> {
  await http.delete(`/proxy/certificates/${id}`);
}

/**
 * listRedirects returns every redirect rule, newest first, or one
 * application's rules when applicationId is given.
 */
export async function listRedirects(
  applicationId?: string,
): Promise<DomainRedirect[]> {
  const response = await http.get<RedirectListEnvelope>("/proxy/redirects", {
    params: applicationId ? { application_id: applicationId } : undefined,
  });
  return response.data.redirects ?? [];
}

/** createRedirect stores one rule (POST → 201). */
export async function createRedirect(
  input: CreateRedirectInput,
): Promise<DomainRedirect> {
  const response = await http.post<RedirectEnvelope>("/proxy/redirects", input);
  return response.data.redirect;
}

/** updateRedirect applies a partial update (PATCH → 200). */
export async function updateRedirect(
  id: string,
  input: UpdateRedirectInput,
): Promise<DomainRedirect> {
  const response = await http.patch<RedirectEnvelope>(
    `/proxy/redirects/${id}`,
    input,
  );
  return response.data.redirect;
}

/** deleteRedirect removes one rule (DELETE → 204). */
export async function deleteRedirect(id: string): Promise<void> {
  await http.delete(`/proxy/redirects/${id}`);
}

/**
 * One generated Traefik router as reported by GET /v1/proxy/routers (see
 * internal/proxy/routers.go): the host rule, the backend service, the
 * entrypoints, the TLS resolver (empty for plain HTTP) and the owning
 * control-plane row (an application, a compose service, or a redirect rule).
 */
export type RouterKind = "application" | "service" | "redirect";

/** Node sync state observed on read (generated vs last synced version). */
export type RouterSyncStatus = "synced" | "pending" | "unknown";

export interface ProxyRouter {
  host: string;
  rule: string;
  service: string;
  target?: string;
  entrypoints: string[];
  middlewares?: string[];
  tls_resolver?: string;
  kind: RouterKind;
  owner_id: string;
  owner_name?: string;
  server_id: string;
  server_name?: string;
}

/** One node's router read outcome; error means the node could not be read. */
export interface RouterNodeState {
  server_id: string;
  server_name?: string;
  sync_status: RouterSyncStatus;
  error?: string;
  diagnostics?: Array<{
    application_id: string;
    kind?: string;
    domain?: string;
    reason: string;
  }>;
}

interface RouterListEnvelope {
  routers: ProxyRouter[];
  nodes: RouterNodeState[];
}

/**
 * listRouters returns the generated Traefik routers of one node, or every
 * node when serverId is omitted. An unreachable node is reported on its node
 * entry (or answers 502 for a single-node read) — never as invented rows.
 */
export async function listRouters(serverId?: string): Promise<RouterListEnvelope> {
  const response = await http.get<RouterListEnvelope>("/proxy/routers", {
    params: serverId ? { server_id: serverId } : undefined,
  });
  return {
    routers: response.data.routers ?? [],
    nodes: response.data.nodes ?? [],
  };
}

/**
 * toCertificateInput maps a draft onto the wire body. http-01 never names a
 * provider and never asks for a wildcard; dns-01 always names one.
 */
export function toCertificateInput(
  draft: CertificateDraft,
): UpdateCertificateInput {
  if (draft.challenge === "dns-01") {
    return {
      enabled: draft.enabled,
      challenge: draft.challenge,
      dns_provider_id: draft.dns_provider_id,
      wildcard: draft.wildcard,
    };
  }
  return { enabled: draft.enabled, challenge: draft.challenge };
}

/** draftFromCertificate seeds the editor from a stored configuration. */
export function draftFromCertificate(certificate: Certificate): CertificateDraft {
  return {
    application_id: certificate.application_id,
    challenge: certificate.challenge,
    dns_provider_id: certificate.dns_provider_id,
    wildcard: certificate.wildcard,
    enabled: certificate.enabled,
  };
}

/** providerLabel renders the human name of a provider type. */
export function providerLabel(provider: DNSProviderName): string {
  return provider === "cloudflare" ? "Cloudflare" : "DigitalOcean";
}

/**
 * certificateStatusLabel renders the observed status; undefined means the
 * API reported none (no status service configured) and is never invented.
 * Resolved in the current locale at invocation time; the English literals
 * below are the fallback when the catalog is not registered.
 */
export function certificateStatusLabel(
  status: CertificateStatus | undefined,
): string {
  switch (status) {
    case "present":
      return proxyText("domains.certificates.statusPresent", "present");
    case "absent":
      return proxyText("domains.certificates.statusAbsent", "no certificate");
    case "unknown":
      return proxyText("domains.certificates.statusUnknown", "unknown");
    default:
      return proxyText(
        "domains.certificates.statusNotReported",
        "not reported",
      );
  }
}

/** certificateStatusTagType maps the observed status onto a tag style. */
export function certificateStatusTagType(
  status: CertificateStatus | undefined,
): "success" | "warning" | "default" {
  switch (status) {
    case "present":
      return "success";
    case "unknown":
      return "warning";
    default:
      return "default";
  }
}

/**
 * describeProxyError maps a thrown error to a user-facing message. The
 * backend returns actionable text for 400/409/503 (see writeSSLError), so a
 * present message wins over the generic fallback. Classification uses the
 * raw status and raw message only; curated summaries resolve in the current
 * locale at invocation time, and the raw diagnostic is preserved verbatim
 * whenever it carries the actionable detail.
 */
export function describeProxyError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return proxyText(
        "domains.errors.sessionExpired",
        "Your session expired. Please sign in again.",
      );
    }
    if (error.status === 403) {
      return proxyText(
        "domains.errors.adminScope",
        "You need the admin scope to manage domains and SSL. Sign in with an admin account or use an admin API token.",
      );
    }
    if (error.status === 400 || error.status === 422) {
      return (
      stripErrorPrefix(error.message) ||
      proxyText(
        "domains.errors.invalidRequest",
        "Invalid request. Check the highlighted fields and retry.",
      )
    );
    }
    if (error.status === 404) {
      return proxyText(
        "domains.errors.notFound",
        "Not found. It may have been deleted already.",
      );
    }
    if (error.status === 409) {
      return (
        stripErrorPrefix(error.message) ||
        proxyText(
          "domains.errors.conflict",
          "The change conflicts with existing state.",
        )
      );
    }
    if (error.status === 502) {
      return proxyText(
        "domains.errors.nodeUnreachable",
        "The node agent is unreachable. Check the node status and retry.",
      );
    }
    if (error.status === 503) {
      return (
        stripErrorPrefix(error.message) ||
        proxyText(
          "domains.errors.secretNotConfigured",
          "The deployment secret is not configured, so credentials cannot " +
            "be stored (set GOTHAM_SECRET_KEY).",
        )
      );
    }
    return withProxyStatusDiagnostic(stripErrorPrefix(error.message));
  }
  if (error instanceof Error) {
    return withProxyDiagnostic(stripErrorPrefix(error.message));
  }
  return proxyText(
    "domains.errors.unexpected",
    "Something went wrong. Please try again.",
  );
}

/**
 * withProxyStatusDiagnostic pairs an unknown API failure's raw diagnostic
 * with the localized request summary (`<summary>: <raw>`). An empty or
 * already-generic diagnostic renders the summary alone.
 */
function withProxyStatusDiagnostic(raw: string): string {
  const summary = proxyText("domains.errors.requestFailed", "Request failed");
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

/**
 * withProxyDiagnostic pairs an unknown failure's raw diagnostic with a
 * localized summary (`<summary>: <raw>`), derived reactively from the raw
 * error plus the current locale. An empty or already-generic diagnostic
 * renders the summary alone, never `Request failed: Request failed`.
 * Known refusal branches above keep their raw actionable text untouched.
 */
function withProxyDiagnostic(raw: string): string {
  const summary = proxyText(
    "domains.errors.unexpected",
    "Something went wrong. Please try again.",
  );
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}
