import { defineStore } from "pinia";
import { ref } from "vue";

import { listApplications } from "../api/applications";
import type { Application } from "../api/applications";
import {
  createCertificate,
  createDNSProvider,
  deleteCertificate,
  deleteDNSProvider,
  describeProxyError,
  listCertificates,
  listDNSProviders,
  updateCertificate,
  updateDNSProvider,
} from "../api/proxy";
import type {
  Certificate,
  CreateCertificateInput,
  CreateDNSProviderInput,
  DNSProvider,
  UpdateCertificateInput,
  UpdateDNSProviderInput,
} from "../api/proxy";

export const useProxyStore = defineStore("proxy", () => {
  const providers = ref<DNSProvider[]>([]);
  const certificates = ref<Certificate[]>([]);
  const applications = ref<Application[]>([]);
  const loading = ref(false);
  const certificatesLoading = ref(false);
  const error = ref<string | null>(null);
  const certificatesError = ref<string | null>(null);

  /** fetchProviders loads every DNS provider (credentials never returned). */
  async function fetchProviders(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      providers.value = await listDNSProviders();
    } catch (err) {
      error.value = describeProxyError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** fetchCertificates loads every certificate configuration. */
  async function fetchCertificates(): Promise<void> {
    certificatesLoading.value = true;
    certificatesError.value = null;
    try {
      certificates.value = await listCertificates();
    } catch (err) {
      certificatesError.value = describeProxyError(err);
      throw err;
    } finally {
      certificatesLoading.value = false;
    }
  }

  /** fetchApplications loads the applications certificates can target. */
  async function fetchApplications(): Promise<void> {
    try {
      applications.value = await listApplications();
    } catch (err) {
      error.value = describeProxyError(err);
      throw err;
    }
  }

  /** providerOf returns one provider from the cache, if loaded. */
  function providerOf(providerId: string): DNSProvider | null {
    return providers.value.find((item) => item.id === providerId) ?? null;
  }

  /** certificateOf returns the application's certificate config, if any. */
  function certificateOf(applicationId: string): Certificate | null {
    return (
      certificates.value.find((item) => item.application_id === applicationId) ??
      null
    );
  }

  /** applicationOf returns one application from the cache, if loaded. */
  function applicationOf(applicationId: string): Application | null {
    return applications.value.find((item) => item.id === applicationId) ?? null;
  }

  /** createProvider stores a provider and refreshes the list. */
  async function createProvider(
    input: CreateDNSProviderInput,
  ): Promise<DNSProvider> {
    const created = await createDNSProvider(input);
    await fetchProviders();
    return created;
  }

  /** updateProvider patches a provider and refreshes the list. */
  async function updateProvider(
    id: string,
    input: UpdateDNSProviderInput,
  ): Promise<DNSProvider> {
    const updated = await updateDNSProvider(id, input);
    await fetchProviders();
    return updated;
  }

  /** removeProvider deletes a provider and refreshes the list. */
  async function removeProvider(id: string): Promise<void> {
    await deleteDNSProvider(id);
    await fetchProviders();
  }

  /** createCertificate stores a configuration and refreshes the list. */
  async function createCertificateConfig(
    input: CreateCertificateInput,
  ): Promise<Certificate> {
    const created = await createCertificate(input);
    await fetchCertificates();
    return created;
  }

  /** updateCertificateConfig patches a configuration and refreshes the list. */
  async function updateCertificateConfig(
    id: string,
    input: UpdateCertificateInput,
  ): Promise<Certificate> {
    const updated = await updateCertificate(id, input);
    await fetchCertificates();
    return updated;
  }

  /** removeCertificate deletes a configuration and refreshes the list. */
  async function removeCertificate(id: string): Promise<void> {
    await deleteCertificate(id);
    await fetchCertificates();
  }

  return {
    providers,
    certificates,
    applications,
    loading,
    certificatesLoading,
    error,
    certificatesError,
    fetchProviders,
    fetchCertificates,
    fetchApplications,
    providerOf,
    certificateOf,
    applicationOf,
    createProvider,
    updateProvider,
    removeProvider,
    createCertificateConfig,
    updateCertificateConfig,
    removeCertificate,
  };
});
