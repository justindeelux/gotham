import { inject, provide, ref, type Ref } from "vue";

/** DomainsRefreshKey shares the alias-mutation tick between the sibling panels. */
const DomainsRefreshKey = Symbol("application-domains-refresh");

/**
 * provideDomainsRefresh creates the tick the Domains tab bumps after every
 * alias mutation. DomainEditor calls it once; the alias panel bumps, the
 * certificate panel watches.
 */
export function provideDomainsRefresh(): Ref<number> {
  const tick = ref(0);
  provide(DomainsRefreshKey, tick);
  return tick;
}

/** useDomainsRefresh returns the shared tick, or null outside DomainEditor. */
export function useDomainsRefresh(): Ref<number> | null {
  return inject<Ref<number> | null>(DomainsRefreshKey, null);
}

/** bumpDomainsRefresh notifies the certificate panel that the rows changed. */
export function bumpDomainsRefresh(): void {
  const tick = useDomainsRefresh();
  if (tick !== null) {
    tick.value += 1;
  }
}
