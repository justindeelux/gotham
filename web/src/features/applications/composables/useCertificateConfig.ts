import { useMessage } from "naive-ui";
import { activeLocale, i18n } from "@/shared/i18n";
import { computed, onMounted, ref, watch, type Ref } from "vue";

import {
  describeProxyError,
  draftFromCertificate,
  providerLabel,
  toCertificateInput,
  useProxyStore,
} from "@/features/domains";
import type { Certificate, CertificateDraft, DNSProvider } from "@/features/domains";

export interface CertificateOwner {
  kind: "application" | "service";
  id: string;
  name?: string;
}

function ownerCertificate(certificate: Certificate, owner: CertificateOwner): boolean {
  if (owner.kind === "service") {
    return certificate.service_id === owner.id;
  }
  return certificate.application_id === owner.id;
}

export function useCertificateConfig(
  owner: Ref<CertificateOwner>,
  domain: Ref<string>,
) {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const certificateSaving = ref(false);
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
    const current = owner.value;
    return {
      application_id: current.kind === "application" ? current.id : "",
      service_id: current.kind === "service" ? current.id : "",
      challenge: "http-01",
      dns_provider_id: "",
      wildcard: false,
      enabled: true,
    };
  }

  const certificate = computed<Certificate | null>(() => {
    const host = domain.value;
    if (host === "") {
      return null;
    }
    return (
      proxyStore.certificates.find(
        (item) => item.domain === host && ownerCertificate(item, owner.value),
      ) ?? null
    );
  });

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

  function openCertificateEdit(): void {
    certificateErrorRaw.value = null;
    const existing = certificate.value;
    certificateDraft.value = existing
      ? draftFromCertificate(existing)
      : emptyDraft();
  }

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
        const current = owner.value;
        await proxyStore.createCertificateConfig({
          ...input,
          ...(current.kind === "service"
            ? { service_id: current.id }
            : { application_id: current.id }),
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

  function tr(key: string): string {
    void activeLocale.value;
    return String(i18n.global.t(key));
  }

  async function load(): Promise<void> {
    await Promise.allSettled([
      proxyStore.fetchProviders(),
      proxyStore.fetchCertificates(),
    ]);
  }

  watch(
    () => owner.value.id,
    () => {
      certificateErrorRaw.value = null;
      certificateDraft.value = emptyDraft();
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
