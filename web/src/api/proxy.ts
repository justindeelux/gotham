import { http } from "./http";
import { isApiError } from "./servers";

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
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * Credentials are write-only: the API never returns a sealed credential, only
 * `credentials_set`. The SPA may set or rotate one and must never render,
 * log or persist it. Certificate responses carry no issuance status and no
 * expiry — the API records the desired configuration only.
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

/** One certificate configuration as returned by the API. */
export interface Certificate {
  id: string;
  application_id: string;
  /** Recorded host, copied from the application's base_domain at write time. */
  domain: string;
  enabled: boolean;
  challenge: ChallengeMode;
  /** Empty for http-01. */
  dns_provider_id: string;
  wildcard: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * Body of POST /proxy/certificates. The recorded domain always comes from the
 * application's base_domain, never from the body.
 */
export interface CreateCertificateInput {
  application_id: string;
  enabled?: boolean;
  challenge?: ChallengeMode;
  dns_provider_id?: string;
  wildcard?: boolean;
}

/** Body of PATCH /proxy/certificates/{id}. Omitted fields stay unchanged. */
export interface UpdateCertificateInput {
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
 * describeProxyError maps a thrown error to a user-facing message. The
 * backend returns actionable text for 400/409/503 (see writeSSLError), so a
 * present message wins over the generic fallback.
 */
export function describeProxyError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 403) {
      return "You need the admin scope to manage domains and SSL. Sign in with an admin account or use an admin API token.";
    }
    if (error.status === 400 || error.status === 422) {
      return error.message || "Invalid request. Check the highlighted fields and retry.";
    }
    if (error.status === 404) {
      return "Not found. It may have been deleted already.";
    }
    if (error.status === 409) {
      return error.message || "The change conflicts with existing state.";
    }
    if (error.status === 502) {
      return "The node agent is unreachable. Check the node status and retry.";
    }
    if (error.status === 503) {
      return (
        error.message ||
        "The deployment secret is not configured, so credentials cannot be stored (set GOTHAM_SECRET_KEY)."
      );
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}
