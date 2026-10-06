<script setup lang="ts">
import { useDomainsOverview } from "@/features/domains/composables/useDomainsOverview";
import { useProxyStore } from "@/features/domains/stores/proxy";

const proxyStore = useProxyStore();
const {
  enabledProviders,
  applicationsWithDomain,
  enabledCertificates,
  wildcardCertificates,
  statText,
} = useDomainsOverview();
</script>

<template>
  <div class="grid cols-4 kpi-row">
    <div class="stat">
      <p class="stat-label">{{ $t("domains.stats.certConfigs") }}</p>
      <p class="stat-value num">{{ statText(proxyStore.certificates.length) }}</p>
      <p class="stat-sub">{{ $t("domains.stats.certConfigsSub", { count: statText(enabledCertificates) }) }}</p>
    </div>
    <div class="stat">
      <p class="stat-label">{{ $t("domains.stats.wildcardConfigs") }}</p>
      <p class="stat-value num">{{ statText(wildcardCertificates) }}</p>
      <p class="stat-sub">{{ $t("domains.stats.wildcardSub") }}</p>
    </div>
    <div class="stat">
      <p class="stat-label">{{ $t("domains.stats.dnsProviders") }}</p>
      <p class="stat-value num">{{ statText(proxyStore.providers.length) }}</p>
      <p class="stat-sub">{{ $t("domains.stats.dnsProvidersSub", { count: statText(enabledProviders.length) }) }}</p>
    </div>
    <div class="stat">
      <p class="stat-label">{{ $t("domains.stats.appsWithDomain") }}</p>
      <p class="stat-value num">{{ statText(applicationsWithDomain) }}</p>
      <p class="stat-sub">{{ $t("domains.stats.appsWithDomainSub") }}</p>
    </div>
  </div>
</template>

<style scoped>
.grid {
  display: grid;
  gap: var(--space-4);
}

.cols-4 {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
}

.stat-label {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
}

.stat-value {
  font-family: var(--font-display);
  font-size: var(--text-3xl);
  font-weight: 700;
  line-height: 1.1;
  letter-spacing: -0.02em;
  color: var(--fg-2);
  margin-top: 6px;
}

.stat-sub {
  font-size: var(--text-xs);
  color: var(--muted);
  margin-top: 6px;
}

@media (max-width: 1024px) {
  .cols-4 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .cols-4 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
