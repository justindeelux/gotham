import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";

import { useProxyStore } from "@/features/domains/stores/proxy";

function createDomainsOverviewState() {
  const proxyStore = useProxyStore();

  const statsReady = ref(false);

  const enabledProviders = computed(() =>
    proxyStore.providers.filter((provider) => provider.enabled),
  );

  const applicationsWithDomain = computed<number>(
    () => proxyStore.applications.filter((item) => item.primary_domain !== "").length,
  );

  const enabledCertificates = computed<number>(
    () => proxyStore.certificates.filter((item) => item.enabled).length,
  );

  const wildcardCertificates = computed<number>(
    () => proxyStore.certificates.filter((item) => item.wildcard).length,
  );

  const statsBlocked = computed<boolean>(
    () =>
      !statsReady.value ||
      proxyStore.error !== null ||
      proxyStore.certificatesError !== null,
  );

  function statText(count: number): string {
    return statsBlocked.value ? "—" : String(count);
  }

  async function load(): Promise<void> {
    try {
      await Promise.allSettled([
        proxyStore.fetchProviders(),
        proxyStore.fetchCertificates(),
        proxyStore.fetchRedirects(),
        proxyStore.fetchRouters(),
        proxyStore.fetchApplications(),
      ]);
    } finally {
      statsReady.value = true;
    }
  }

  return {
    enabledProviders,
    applicationsWithDomain,
    enabledCertificates,
    wildcardCertificates,
    statsBlocked,
    statText,
    load,
  };
}

export type DomainsOverviewState = ReturnType<typeof createDomainsOverviewState>;

const domainsOverviewKey: InjectionKey<DomainsOverviewState> =
  Symbol("domains.overview");

export function provideDomainsOverview(): DomainsOverviewState {
  const state = createDomainsOverviewState();
  provide(domainsOverviewKey, state);
  return state;
}

export function useDomainsOverview(): DomainsOverviewState {
  const state = inject(domainsOverviewKey);
  if (!state) {
    throw new Error("useDomainsOverview must be used inside a page providing it.");
  }
  return state;
}
