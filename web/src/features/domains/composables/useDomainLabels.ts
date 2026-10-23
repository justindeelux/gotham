import { providerLabel, proxyText } from "@/features/domains/api/proxy";
import type { Certificate } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

export function useDomainLabels() {
  const proxyStore = useProxyStore();

  function providerName(providerId: string): string {
    if (!providerId) {
      return "—";
    }
    const provider = proxyStore.providerOf(providerId);
    return provider
      ? provider.name || providerLabel(provider.provider)
      : proxyText("domains.providers.unknownProvider", "unknown provider");
  }

  function applicationName(applicationId: string): string {
    return proxyStore.applicationOf(applicationId)?.name ?? applicationId.slice(0, 8);
  }

  function applicationDomain(certificate: Certificate): string {
    return proxyStore.applicationOf(certificate.application_id)?.primary_domain ?? "";
  }

  return { providerName, applicationName, applicationDomain };
}
