import { inject, provide, ref, type Ref } from "vue";

const DomainsRefreshKey = Symbol("application-domains-refresh");

export function provideDomainsRefresh(): Ref<number> {
  const tick = ref(0);
  provide(DomainsRefreshKey, tick);
  return tick;
}

export function useDomainsRefresh(): Ref<number> | null {
  return inject<Ref<number> | null>(DomainsRefreshKey, null);
}

export function bumpDomainsRefresh(tick: Ref<number> | null | undefined): void {
  if (tick != null) {
    tick.value += 1;
  }
}
