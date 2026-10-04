import { useMessage } from "naive-ui";
import { computed, onMounted, ref, watch, type Ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import {
  describeProxyError,
  draftFromCertificate,
  providerLabel,
  toCertificateInput,
  useProxyStore,
} from "@/features/domains";
import type { Certificate, CertificateDraft, DNSProvider } from "@/features/domains";

/**
 * Certificate-configuration editing behind the Domains tab. The certificate's
 * recorded domain always comes from the application's current base_domain, so
 * a domain change is flagged until the configuration is saved again.
 */
export function useCertificateConfig(application: Ref<Application>) {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const certificateOpen = ref(false);
  const certificateSaving = ref(false);
  const certificateError = ref<string | null>(null);
  const certificateDraft = ref<CertificateDraft>({
    application_id: application.value.id,
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    enabled: true,
  });

  const certificate = computed<Certificate | null>(() =>
    proxyStore.certificateOf(application.value.id),
  );

  /** recordedDomainDiffers flags a base domain changed after the last save. */
  const recordedDomainDiffers = computed<boolean>(
    () =>
      certificate.value !== null &&
      certificate.value.domain !== application.value.base_domain,
  );

  /** providerName resolves a stored provider id to its display label. */
  function providerName(providerId: string): string {
    if (!providerId) {
      return "shared HTTP resolver";
    }
    const provider: DNSProvider | null = proxyStore.providerOf(providerId);
    if (!provider) {
      return "unknown provider";
    }
    return `${provider.name || providerLabel(provider.provider)} · ${provider.provider}`;
  }

  /** openCertificateEdit seeds the dialog from the stored configuration. */
  function openCertificateEdit(): void {
    certificateError.value = null;
    const existing = certificate.value;
    certificateDraft.value = existing
      ? draftFromCertificate(existing)
      : {
          application_id: application.value.id,
          challenge: "http-01",
          dns_provider_id: "",
          wildcard: false,
          enabled: true,
        };
    certificateOpen.value = true;
  }

  /** handleSaveCertificate creates or updates the configuration. */
  async function handleSaveCertificate(): Promise<void> {
    certificateError.value = null;
    certificateSaving.value = true;
    try {
      const input = toCertificateInput(certificateDraft.value);
      const existing = certificate.value;
      if (existing) {
        await proxyStore.updateCertificateConfig(existing.id, input);
        message.success("Certificate configuration saved.");
      } else {
        await proxyStore.createCertificateConfig({
          ...input,
          application_id: application.value.id,
        });
        message.success("Certificate configuration created.");
      }
      certificateOpen.value = false;
    } catch (error) {
      certificateError.value = describeProxyError(error);
    } finally {
      certificateSaving.value = false;
    }
  }

  /** handleRerecordDomain re-saves the configuration to record the new domain. */
  async function handleRerecordDomain(): Promise<void> {
    const existing = certificate.value;
    if (!existing) {
      return;
    }
    certificateError.value = null;
    try {
      await proxyStore.updateCertificateConfig(existing.id, {});
      message.success("Certificate re-recorded the current application domain.");
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  /** handleDeleteCertificate removes the configuration. */
  async function handleDeleteCertificate(): Promise<void> {
    const existing = certificate.value;
    if (!existing) {
      return;
    }
    try {
      await proxyStore.removeCertificate(existing.id);
      message.success("Certificate configuration deleted. The route stays HTTP-only.");
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  /** load refreshes the providers and certificates the editor depends on. */
  async function load(): Promise<void> {
    await Promise.allSettled([
      proxyStore.fetchProviders(),
      proxyStore.fetchCertificates(),
    ]);
  }

  watch(
    () => application.value.id,
    () => {
      certificateError.value = null;
      void load();
    },
  );

  onMounted(() => {
    void load();
  });

  return {
    proxyStore,
    certificate,
    certificateOpen,
    certificateSaving,
    certificateError,
    certificateDraft,
    recordedDomainDiffers,
    providerName,
    openCertificateEdit,
    handleSaveCertificate,
    handleRerecordDomain,
    handleDeleteCertificate,
  };
}
