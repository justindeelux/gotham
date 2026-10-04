import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";

import { useProxyStore } from "@/features/domains/stores/proxy";

/**
 * Overview state of the Domains page: first-load orchestration and the
 * KPI tile counts. State is per page instance (created by provideDomainsOverview
 * in the page, shared via inject) so a revisit starts clean: the tiles show
 * a dash until the first load settles instead of stale counts.
 */

function createDomainsOverviewState() {
  const proxyStore = useProxyStore();

  /**
   * statsReady flips once the first load settles, so the tiles never flash a
   * 0 that was never read.
   */
  const statsReady = ref(false);

  const enabledProviders = computed(() =>
    proxyStore.providers.filter((provider) => provider.enabled),
  );

  const applicationsWithDomain = computed<number>(
    () => proxyStore.applications.filter((item) => item.base_domain !== "").length,
  );

  const enabledCertificates = computed<number>(
    () => proxyStore.certificates.filter((item) => item.enabled).length,
  );

  const wildcardCertificates = computed<number>(
    () => proxyStore.certificates.filter((item) => item.wildcard).length,
  );

  /**
   * statsBlocked is true while the first load is in flight or when a read
   * failed (for example a non-admin 403): the tiles then show a dash rather
   * than a 0 that looks like real data.
   */
  const statsBlocked = computed<boolean>(
    () =>
      !statsReady.value ||
      proxyStore.error !== null ||
      proxyStore.certificatesError !== null,
  );

  /** statText renders a tile count, or a dash when the read failed/is pending. */
  function statText(count: number): string {
    return statsBlocked.value ? "—" : String(count);
  }

  /** load refreshes providers, certificates, redirects and the name map. */
  async function load(): Promise<void> {
    try {
      await Promise.allSettled([
        proxyStore.fetchProviders(),
        proxyStore.fetchCertificates(),
        proxyStore.fetchRedirects(),
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

/**
 * provideDomainsOverview creates the overview state for one page mount.
 * Call once in the page; descendants share it through useDomainsOverview.
 */
export function provideDomainsOverview(): DomainsOverviewState {
  const state = createDomainsOverviewState();
  provide(domainsOverviewKey, state);
  return state;
}

/** useDomainsOverview shares the page instance; call in descendant components. */
export function useDomainsOverview(): DomainsOverviewState {
  const state = inject(domainsOverviewKey);
  if (!state) {
    throw new Error("useDomainsOverview must be used inside a page providing it.");
  }
  return state;
}
