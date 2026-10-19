import { useMessage } from "naive-ui";
import { activeLocale, i18n } from "@/shared/i18n";
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
 * Certificate configuration for exactly one domain row (JUS-236). The proxy
 * API records one intent per hostname (JUS-89), so every action here reads
 * and writes the intent of the domain the user clicked — never the primary
 * domain implicitly.
 */
export function useCertificateConfig(
  application: Ref<Application>,
  domain: Ref<string>,
) {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const certificateSaving = ref(false);
  /**
   * certificateErrorRaw keeps the failure behind the dialog banner;
   * certificateError derives its display text in the current locale, so a
   * language switch refreshes a retained failure without resubmitting.
   */
  const certificateErrorRaw = ref<unknown>(null);
  const certificateError = computed<string | null>(() => {
    if (certificateErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeProxyError(certificateErrorRaw.value);
  });
  const certificateDraft = ref<CertificateDraft>(emptyDraft());

  function emptyDraft(): CertificateDraft {
    return {
      application_id: application.value.id,
      challenge: "http-01",
      dns_provider_id: "",
      wildcard: false,
      enabled: true,
    };
  }

  /** certificate is the intent recorded for the selected domain only. */
  const certificate = computed<Certificate | null>(() => {
    const host = domain.value;
    if (host === "") {
      return null;
    }
    return (
      proxyStore.certificates.find(
        (item) => item.application_id === application.value.id && item.domain === host,
      ) ?? null
    );
  });

  /** providerName resolves a stored provider id to its display label. */
  function providerName(providerId: string): string {
    if (!providerId) {
      return tr("applications.cert.sharedResolver");
    }
    const provider: DNSProvider | null = proxyStore.providerOf(providerId);
    if (!provider) {
      return tr("applications.cert.unknownProvider");
    }
    return `${provider.name || providerLabel(provider.provider)} · ${provider.provider}`;
  }

  /** openCertificateEdit seeds the dialog from the selected domain's intent. */
  function openCertificateEdit(): void {
    certificateErrorRaw.value = null;
    const existing = certificate.value;
    certificateDraft.value = existing
      ? draftFromCertificate(existing)
      : emptyDraft();
  }

  /** handleSaveCertificate creates or updates the selected domain's intent. */
  async function handleSaveCertificate(): Promise<void> {
    certificateErrorRaw.value = null;
    certificateSaving.value = true;
    try {
      const input = toCertificateInput(certificateDraft.value);
      const existing = certificate.value;
      if (existing) {
        await proxyStore.updateCertificateConfig(existing.id, input);
        message.success(tr("applications.detail.certSaved"));
      } else {
        await proxyStore.createCertificateConfig({
          ...input,
          application_id: application.value.id,
          domain: domain.value,
        });
        message.success(tr("applications.detail.certCreated"));
      }
    } catch (error) {
      certificateErrorRaw.value = error;
    } finally {
      certificateSaving.value = false;
    }
  }

  /** handleDeleteCertificate removes the selected domain's intent. */
  async function handleDeleteCertificate(): Promise<void> {
    const existing = certificate.value;
    if (!existing) {
      return;
    }
    try {
      await proxyStore.removeCertificate(existing.id);
      message.success(tr("applications.detail.certDeleted"));
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  /**
   * tr resolves one applications message in the current locale. Reading
   * activeLocale pins the caller to the language switch.
   */
  function tr(key: string): string {
    void activeLocale.value;
    return String(i18n.global.t(key));
  }

  /** load refreshes the providers and certificate intents. */
  async function load(): Promise<void> {
    await Promise.allSettled([
      proxyStore.fetchProviders(),
      proxyStore.fetchCertificates(),
    ]);
  }

  watch(
    () => application.value.id,
    () => {
      certificateErrorRaw.value = null;
      void load();
    },
  );

  watch(domain, () => {
    certificateErrorRaw.value = null;
  });

  onMounted(() => {
    void load();
  });

  return {
    proxyStore,
    certificate,
    certificateSaving,
    certificateError,
    certificateDraft,
    providerName,
    openCertificateEdit,
    handleSaveCertificate,
    handleDeleteCertificate,
  };
}
