import { useMessage } from "naive-ui";
import { activeLocale, i18n } from "@/shared/i18n";
import { computed, onMounted, ref, watch, type Ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import { listDomains } from "@/features/applications/api/applications";
import { useDomainsRefresh } from "@/features/applications/composables/domainRefresh";
import {
  describeProxyError,
  draftFromCertificate,
  providerLabel,
  toCertificateInput,
  useProxyStore,
} from "@/features/domains";
import type { Certificate, CertificateDraft, DNSProvider } from "@/features/domains";

/**
 * Certificate-configuration editing behind the Domains tab. One intent
 * exists per domain (JUS-89): this editor manages the primary domain's
 * intent, while an intent recorded for a detached host is flagged until it
 * is explicitly re-recorded onto the primary domain.
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

  /**
   * certificate prefers the primary domain's intent; a stale intent recorded
   * for a detached host is shown (with the re-record banner) when no primary
   * intent exists.
   */
  const certificate = computed<Certificate | null>(() => {
    const configs = proxyStore.certificates.filter(
      (item) => item.application_id === application.value.id,
    );
    return (
      configs.find((item) => item.domain === application.value.base_domain) ??
      configs[0] ??
      null
    );
  });

  /** attachedDomains lists the hostnames currently attached to the app. */
  const attachedDomains = ref<string[]>([]);

  /** loadDomains refreshes the attached hostnames the banner compares. */
  async function loadDomains(): Promise<void> {
    try {
      const rows = await listDomains(application.value.id);
      attachedDomains.value = rows.map((row) => row.domain);
    } catch {
      attachedDomains.value = [application.value.base_domain].filter(
        (domain) => domain !== "",
      );
    }
  }

  /** recordedDomainDiffers flags an intent recorded for a detached host. */
  const recordedDomainDiffers = computed<boolean>(() => {
    const current = certificate.value;
    if (current === null || attachedDomains.value.length === 0) {
      return false;
    }
    return !attachedDomains.value.includes(current.domain);
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

  /** handleRerecordDomain re-targets a detached intent onto the primary domain. */
  async function handleRerecordDomain(): Promise<void> {
    const existing = certificate.value;
    if (!existing) {
      return;
    }
    certificateErrorRaw.value = null;
    try {
      await proxyStore.updateCertificateConfig(existing.id, {
        domain: application.value.base_domain,
      });
      await load();
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

  /** load refreshes the providers, certificates and attached domains. */
  async function load(): Promise<void> {
    await Promise.allSettled([
      proxyStore.fetchProviders(),
      proxyStore.fetchCertificates(),
      loadDomains(),
    ]);
  }

  watch(
    () => application.value.id,
    () => {
      certificateErrorRaw.value = null;
      void load();
    },
  );

  /**
   * A same-page base-domain save keeps the application id but changes the
   * mirror: reload the rows so the re-record banner appears immediately.
   * Alias mutations bump the shared tick instead (the mirror may not move).
   */
  watch(
    () => application.value.base_domain,
    () => {
      void loadDomains();
    },
  );

  const domainsTick = useDomainsRefresh();
  if (domainsTick != null) {
    watch(domainsTick, () => {
      void loadDomains();
    });
  }

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
