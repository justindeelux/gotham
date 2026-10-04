import { useMessage } from "naive-ui";
import { ref } from "vue";

import {
  describeProxyError,
  draftFromCertificate,
  toCertificateInput,
} from "@/features/domains/api/proxy";
import type { Certificate, CertificateDraft } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

/** Dialog state is module-scoped: the page head, the certificates panel and the dialog share it. */
const certificateOpen = ref(false);
const certificateSaving = ref(false);
const certificateError = ref<string | null>(null);
const editingCertificate = ref<Certificate | null>(null);
const certificateDraft = ref<CertificateDraft>(emptyCertificateDraft());

/** emptyCertificateDraft returns a create-mode certificate draft. */
export function emptyCertificateDraft(): CertificateDraft {
  return {
    application_id: "",
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    enabled: true,
  };
}

/** useCertificates owns the certificate dialog, saves and deletions. */
export function useCertificates() {
  const message = useMessage();
  const proxyStore = useProxyStore();

  /** openCertificateCreate resets the dialog for a new configuration. */
  function openCertificateCreate(): void {
    editingCertificate.value = null;
    certificateDraft.value = emptyCertificateDraft();
    certificateError.value = null;
    certificateOpen.value = true;
  }

  /** openCertificateEdit seeds the dialog from a stored configuration. */
  function openCertificateEdit(certificate: Certificate): void {
    editingCertificate.value = certificate;
    certificateDraft.value = draftFromCertificate(certificate);
    certificateError.value = null;
    certificateOpen.value = true;
  }

  /** handleSaveCertificate creates or patches one certificate configuration. */
  async function handleSaveCertificate(): Promise<void> {
    certificateError.value = null;
    certificateSaving.value = true;
    try {
      const input = toCertificateInput(certificateDraft.value);
      const existing = editingCertificate.value;
      if (existing) {
        await proxyStore.updateCertificateConfig(existing.id, input);
        message.success("Certificate configuration saved.");
      } else {
        await proxyStore.createCertificateConfig({
          ...input,
          application_id: certificateDraft.value.application_id,
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

  /** handleDeleteCertificate removes one configuration. */
  async function handleDeleteCertificate(certificate: Certificate): Promise<void> {
    try {
      await proxyStore.removeCertificate(certificate.id);
      message.success("Certificate configuration deleted.");
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
