import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { activeLocale } from "@/shared/i18n";

import { listApplications } from "@/features/applications";
import type { Application } from "@/features/applications";
import {
  createCertificate,
  createDNSProvider,
  createRedirect,
  deleteCertificate,
  deleteDNSProvider,
  deleteRedirect,
  describeProxyError,
  listCertificates,
  listDNSProviders,
  listRedirects,
  listRouters,
  updateCertificate,
  updateDNSProvider,
  updateRedirect,
} from "@/features/domains/api/proxy";
import type {
  Certificate,
  CreateCertificateInput,
  CreateDNSProviderInput,
  CreateRedirectInput,
  DNSProvider,
  DomainRedirect,
  ProxyRouter,
  RouterNodeState,
  UpdateCertificateInput,
  UpdateDNSProviderInput,
  UpdateRedirectInput,
} from "@/features/domains/api/proxy";

export const useProxyStore = defineStore("proxy", () => {
  const providers = ref<DNSProvider[]>([]);
  const certificates = ref<Certificate[]>([]);
  const redirects = ref<DomainRedirect[]>([]);
  const routers = ref<ProxyRouter[]>([]);
  const routerNodes = ref<RouterNodeState[]>([]);
  const applications = ref<Application[]>([]);
  const loading = ref(false);
  const certificatesLoading = ref(false);
  const redirectsLoading = ref(false);
  const routersLoading = ref(false);
  /**
   * Raw failures behind the page alerts. The display strings below derive
   * from these plus the current locale, so a language switch refreshes a
   * retained banner without a refetch; classification always sees the raw
   * error inside describeProxyError, never translated text.
   */
  const errorRaw = ref<unknown>(null);
  const certificatesErrorRaw = ref<unknown>(null);
  const redirectsErrorRaw = ref<unknown>(null);
  const routersErrorRaw = ref<unknown>(null);
  const error = computed<string | null>(() => {
    void activeLocale.value;
    return errorRaw.value === null ? null : describeProxyError(errorRaw.value);
  });
  const certificatesError = computed<string | null>(() => {
    void activeLocale.value;
    return certificatesErrorRaw.value === null
      ? null
      : describeProxyError(certificatesErrorRaw.value);
  });
  const redirectsError = computed<string | null>(() => {
    void activeLocale.value;
    return redirectsErrorRaw.value === null
      ? null
      : describeProxyError(redirectsErrorRaw.value);
  });
  const routersError = computed<string | null>(() => {
    void activeLocale.value;
    return routersErrorRaw.value === null
      ? null
      : describeProxyError(routersErrorRaw.value);
  });

  /** fetchProviders loads every DNS provider (credentials never returned). */
  async function fetchProviders(): Promise<void> {
    loading.value = true;
    errorRaw.value = null;
    try {
      providers.value = await listDNSProviders();
    } catch (err) {
      errorRaw.value = err;
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** fetchCertificates loads every certificate configuration. */
  async function fetchCertificates(): Promise<void> {
    certificatesLoading.value = true;
    certificatesErrorRaw.value = null;
    try {
      certificates.value = await listCertificates();
    } catch (err) {
      certificatesErrorRaw.value = err;
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
      errorRaw.value = err;
      throw err;
    }
  }

  /** fetchRedirects loads every domain redirect rule. */
  async function fetchRedirects(): Promise<void> {
    redirectsLoading.value = true;
    redirectsErrorRaw.value = null;
    try {
      redirects.value = await listRedirects();
    } catch (err) {
      redirectsErrorRaw.value = err;
      throw err;
    } finally {
      redirectsLoading.value = false;
    }
  }

  /** fetchRouters loads the generated Traefik routers of every node. */
  async function fetchRouters(): Promise<void> {
    routersLoading.value = true;
    routersErrorRaw.value = null;
    try {
      const envelope = await listRouters();
      routers.value = envelope.routers;
      routerNodes.value = envelope.nodes;
    } catch (err) {
      routersErrorRaw.value = err;
      throw err;
    } finally {
      routersLoading.value = false;
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

  /** createRedirectRule stores a rule and refreshes the list. */
  async function createRedirectRule(
    input: CreateRedirectInput,
  ): Promise<DomainRedirect> {
    const created = await createRedirect(input);
    await fetchRedirects();
    return created;
  }

  /** updateRedirectRule patches a rule and refreshes the list. */
  async function updateRedirectRule(
    id: string,
    input: UpdateRedirectInput,
  ): Promise<DomainRedirect> {
    const updated = await updateRedirect(id, input);
    await fetchRedirects();
    return updated;
  }

  /** removeRedirectRule deletes a rule and refreshes the list. */
  async function removeRedirectRule(id: string): Promise<void> {
    await deleteRedirect(id);
    await fetchRedirects();
  }

  /**
   * reset drops every cached list, so the next sign-in never sees the
   * previous account's proxy data. Called on sign-out (see the auth store).
   */
  function reset(): void {
    providers.value = [];
    certificates.value = [];
    redirects.value = [];
    routers.value = [];
    routerNodes.value = [];
    applications.value = [];
    loading.value = false;
    certificatesLoading.value = false;
    redirectsLoading.value = false;
    routersLoading.value = false;
    errorRaw.value = null;
    certificatesErrorRaw.value = null;
    redirectsErrorRaw.value = null;
    routersErrorRaw.value = null;
  }

  return {
    providers,
    certificates,
    redirects,
    routers,
    routerNodes,
    applications,
    loading,
    certificatesLoading,
    redirectsLoading,
    routersLoading,
    error,
    certificatesError,
    redirectsError,
    routersError,
    fetchProviders,
    fetchCertificates,
    fetchRedirects,
    fetchRouters,
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
    createRedirectRule,
    updateRedirectRule,
    removeRedirectRule,
    reset,
  };
});
