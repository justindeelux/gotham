import { providerLabel } from "@/features/domains/api/proxy";
import type { Certificate } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

/**
 * Display labels shared by the certificate table and the redirect editor.
 * Each resolves ids against the cached proxy store lists.
 */

/** useDomainLabels exposes provider/application name resolution. */
export function useDomainLabels() {
  const proxyStore = useProxyStore();

  /** providerName resolves a provider id to its display label. */
  function providerName(providerId: string): string {
    if (!providerId) {
      return "—";
    }
    const provider = proxyStore.providerOf(providerId);
    return provider
      ? provider.name || providerLabel(provider.provider)
      : "unknown provider";
  }

  /** applicationName resolves an application id to its display name. */
  function applicationName(applicationId: string): string {
    return proxyStore.applicationOf(applicationId)?.name ?? applicationId.slice(0, 8);
  }

  /** applicationDomain shows the base domain a certificate targets. */
  function applicationDomain(certificate: Certificate): string {
    return proxyStore.applicationOf(certificate.application_id)?.base_domain ?? "";
  }

  return { providerName, applicationName, applicationDomain };
}
