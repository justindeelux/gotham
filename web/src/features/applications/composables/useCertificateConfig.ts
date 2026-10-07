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
 * Certificate-configuration editing behind the Domains tab. The certificate's
 * recorded domain always comes from the application's current base_domain, so
 * a domain change is flagged until the configuration is saved again.
 */
export function useCertificateConfig(application: Ref<Application>) {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const certificateOpen = ref(false);
  const certificateSaving = ref(false);
  /**
   * certificateErrorRaw keeps the failure behind the dialog banner;
   * certificateError derives its display text in the current locale, so a
   * language switch refreshes a retained failure without resubmitting. The
   * shared describeProxyError stays the single formatter: it resolves at
   * display time, so a future locale-aware domains helper flows through with
   * no consumer change.
   */
  const certificateErrorRaw = ref<unknown>(null);
  const certificateError = computed<string | null>(() => {
    if (certificateErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeProxyError(certificateErrorRaw.value);
  });
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
      return tr("applications.cert.sharedResolver");
    }
    const provider: DNSProvider | null = proxyStore.providerOf(providerId);
    if (!provider) {
      return tr("applications.cert.unknownProvider");
    }
    return `${provider.name || providerLabel(provider.provider)} · ${provider.provider}`;
  }

  /** openCertificateEdit seeds the dialog from the stored configuration. */
  function openCertificateEdit(): void {
    certificateErrorRaw.value = null;
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
        });
        message.success(tr("applications.detail.certCreated"));
      }
      certificateOpen.value = false;
    } catch (error) {
      certificateErrorRaw.value = error;
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
    certificateErrorRaw.value = null;
    try {
      await proxyStore.updateCertificateConfig(existing.id, {});
      message.success(tr("applications.detail.certRerecorded"));
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
      certificateErrorRaw.value = null;
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
