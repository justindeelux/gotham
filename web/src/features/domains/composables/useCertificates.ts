import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";
import { useMessage } from "naive-ui";
import { activeLocale } from "@/shared/i18n";

import {
  describeProxyError,
  draftFromCertificate,
  proxyText,
  toCertificateInput,
} from "@/features/domains/api/proxy";
import type { Certificate, CertificateDraft } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

function createCertificatesState() {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const certificateOpen = ref(false);
  const certificateSaving = ref(false);
  const certificateErrorRaw = ref<unknown>(null);
  const certificateError = computed<string | null>(() => {
    void activeLocale.value;
    return certificateErrorRaw.value === null
      ? null
      : describeProxyError(certificateErrorRaw.value);
  });
  const editingCertificate = ref<Certificate | null>(null);
  const certificateDraft = ref<CertificateDraft>(emptyCertificateDraft());

  function openCertificateCreate(): void {
    editingCertificate.value = null;
    certificateDraft.value = emptyCertificateDraft();
    certificateErrorRaw.value = null;
    certificateOpen.value = true;
  }

  function openCertificateEdit(certificate: Certificate): void {
    editingCertificate.value = certificate;
    certificateDraft.value = draftFromCertificate(certificate);
    certificateErrorRaw.value = null;
    certificateOpen.value = true;
  }

  async function handleSaveCertificate(): Promise<void> {
    certificateErrorRaw.value = null;
    certificateSaving.value = true;
    try {
      const input = toCertificateInput(certificateDraft.value);
      const existing = editingCertificate.value;
      if (existing) {
        await proxyStore.updateCertificateConfig(existing.id, input);
        message.success(
          proxyText("domains.certificates.saved", "Certificate configuration saved."),
        );
      } else {
        await proxyStore.createCertificateConfig({
          ...input,
          application_id: certificateDraft.value.application_id,
        });
        message.success(
          proxyText("domains.certificates.created", "Certificate configuration created."),
        );
      }
      certificateOpen.value = false;
    } catch (error) {
      certificateErrorRaw.value = error;
    } finally {
      certificateSaving.value = false;
    }
  }

  async function handleDeleteCertificate(certificate: Certificate): Promise<void> {
    try {
      await proxyStore.removeCertificate(certificate.id);
      message.success(
        proxyText("domains.certificates.deleted", "Certificate configuration deleted."),
      );
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  return {
    certificateOpen,
    certificateSaving,
    certificateError,
    editingCertificate,
    certificateDraft,
    openCertificateCreate,
    openCertificateEdit,
    handleSaveCertificate,
    handleDeleteCertificate,
  };
}

export type CertificatesState = ReturnType<typeof createCertificatesState>;

const certificatesKey: InjectionKey<CertificatesState> = Symbol("domains.certificates");

export function emptyCertificateDraft(): CertificateDraft {
  return {
    application_id: "",
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    enabled: true,
  };
}

export function provideCertificates(): CertificatesState {
  const state = createCertificatesState();
  provide(certificatesKey, state);
  return state;
}

export function useCertificates(): CertificatesState {
  const state = inject(certificatesKey);
  if (!state) {
    throw new Error("useCertificates must be used inside a page providing it.");
  }
  return state;
}
