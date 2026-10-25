import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";

export type TextParams = Record<string, string | number>;

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

export type DNSProviderName = "cloudflare" | "digitalocean";

export type ChallengeMode = "http-01" | "dns-01";

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

export interface CreateDNSProviderInput {
  provider: DNSProviderName;
  name?: string;
  zones: string[];
  credential: string;
  enabled?: boolean;
}

export interface UpdateDNSProviderInput {
  provider?: DNSProviderName;
  name?: string;
  zones?: string[];
  credential?: string;
  enabled?: boolean;
}

export type CertificateStatus = "present" | "absent" | "unknown";

export interface PanelCertificateState {
  status: "issued" | "pending" | "failed";
  not_after?: string;
}

export interface Certificate {
  owner_kind?: "application" | "control_plane";
  panel_state?: PanelCertificateState;
  id: string;
  application_id: string;
  domain: string;
  enabled: boolean;
  challenge: ChallengeMode;
  dns_provider_id: string;
  wildcard: boolean;
  status?: CertificateStatus;
  not_after?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateCertificateInput {
  application_id: string;
  domain?: string;
  enabled?: boolean;
  challenge?: ChallengeMode;
  dns_provider_id?: string;
  wildcard?: boolean;
}

export interface UpdateCertificateInput {
  domain?: string;
  enabled?: boolean;
  challenge?: ChallengeMode;
  dns_provider_id?: string;
  wildcard?: boolean;
}

export interface CertificateDraft {
  application_id: string;
  challenge: ChallengeMode;
  dns_provider_id: string;
  wildcard: boolean;
  enabled: boolean;
}

export type RedirectCode = 301 | 302;

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

export interface CreateRedirectInput {
  application_id: string;
  source_domain: string;
  target_domain: string;
  code?: RedirectCode;
  preserve_path?: boolean;
  enabled?: boolean;
}

export interface UpdateRedirectInput {
  source_domain?: string;
  target_domain?: string;
  code?: RedirectCode;
  preserve_path?: boolean;
  enabled?: boolean;
}

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

export async function listDNSProviders(): Promise<DNSProvider[]> {
  const response = await http.get<DNSProviderListEnvelope>("/proxy/dns-providers");
  return response.data.providers ?? [];
}

export async function createDNSProvider(
  input: CreateDNSProviderInput,
): Promise<DNSProvider> {
  const response = await http.post<DNSProviderEnvelope>(
    "/proxy/dns-providers",
    input,
  );
  return response.data.provider;
}

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

export async function deleteDNSProvider(id: string): Promise<void> {
  await http.delete(`/proxy/dns-providers/${id}`);
}

export async function listCertificates(): Promise<Certificate[]> {
  const response = await http.get<CertificateListEnvelope>("/proxy/certificates");
  return response.data.certificates ?? [];
}

export async function createCertificate(
  input: CreateCertificateInput,
): Promise<Certificate> {
  const response = await http.post<CertificateEnvelope>(
    "/proxy/certificates",
    input,
  );
  return response.data.certificate;
}

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

export async function deleteCertificate(id: string): Promise<void> {
  await http.delete(`/proxy/certificates/${id}`);
}

export async function listRedirects(
  applicationId?: string,
): Promise<DomainRedirect[]> {
  const response = await http.get<RedirectListEnvelope>("/proxy/redirects", {
    params: applicationId ? { application_id: applicationId } : undefined,
  });
  return response.data.redirects ?? [];
}

export async function createRedirect(
  input: CreateRedirectInput,
): Promise<DomainRedirect> {
  const response = await http.post<RedirectEnvelope>("/proxy/redirects", input);
  return response.data.redirect;
}

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

export async function deleteRedirect(id: string): Promise<void> {
  await http.delete(`/proxy/redirects/${id}`);
}

export type RouterKind = "application" | "service" | "redirect" | "control_plane";

export type RouterSyncStatus = "synced" | "pending" | "unknown";

export interface ProxyRouter {
  certificate?: PanelCertificateState;
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

export async function listRouters(serverId?: string): Promise<RouterListEnvelope> {
  const response = await http.get<RouterListEnvelope>("/proxy/routers", {
    params: serverId ? { server_id: serverId } : undefined,
  });
  return {
    routers: response.data.routers ?? [],
    nodes: response.data.nodes ?? [],
  };
}

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

export function draftFromCertificate(certificate: Certificate): CertificateDraft {
  return {
    application_id: certificate.application_id,
    challenge: certificate.challenge,
    dns_provider_id: certificate.dns_provider_id,
    wildcard: certificate.wildcard,
    enabled: certificate.enabled,
  };
}

export function providerLabel(provider: DNSProviderName): string {
  return provider === "cloudflare" ? "Cloudflare" : "DigitalOcean";
}

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
            "be stored. Ask the owner to configure it.",
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

function withProxyStatusDiagnostic(raw: string): string {
  const summary = proxyText("domains.errors.requestFailed", "Request failed");
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

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
