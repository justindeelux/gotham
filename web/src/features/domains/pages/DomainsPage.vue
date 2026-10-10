<script setup lang="ts">
import { NAlert, NButton, NTabPane, NTabs } from "naive-ui";
import { onMounted, ref } from "vue";

import CertificateDialog from "@/features/domains/components/CertificateDialog.vue";
import CertificatesPanel from "@/features/domains/components/CertificatesPanel.vue";
import DnsProvidersPanel from "@/features/domains/components/DnsProvidersPanel.vue";
import DomainsStats from "@/features/domains/components/DomainsStats.vue";
import ProviderDialog from "@/features/domains/components/ProviderDialog.vue";
import RedirectCreateCard from "@/features/domains/components/RedirectCreateCard.vue";
import RedirectEditDialog from "@/features/domains/components/RedirectEditDialog.vue";
import RedirectRulesPanel from "@/features/domains/components/RedirectRulesPanel.vue";
import RoutersPanel from "@/features/domains/components/RoutersPanel.vue";
import { provideCertificates } from "@/features/domains/composables/useCertificates";
import { provideDomainsOverview } from "@/features/domains/composables/useDomainsOverview";
import { provideProviders } from "@/features/domains/composables/useProviders";
import { provideRedirects } from "@/features/domains/composables/useRedirects";
import { useProxyStore } from "@/features/domains/stores/proxy";

/**
 * Domains & SSL page: DNS provider credentials, certificate configurations,
 * domain redirect rules and the generated Traefik router list, all backed by
 * the proxy API. Certificate status/expiry is observed live from the owning
 * node; the router list is regenerated from control-plane state on read and
 * an unreadable node is reported, never invented.
 *
 * Thin route component: the tab shell, page head and KPI tiles live here;
 * every section (panels, dialogs, state) lives in its own component or
 * composable.
 */

const proxyStore = useProxyStore();
// Per page instance: the panels and dialogs share this state via inject,
// and it is dropped on unmount — a revisit starts with closed dialogs,
// blank drafts and pending tiles.
const { load } = provideDomainsOverview();
const { openProviderCreate } = provideProviders();
const { openCertificateCreate } = provideCertificates();
provideRedirects();

const activeTab = ref("certificates");

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="domains-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ $t("domains.page.eyebrow") }}</p>
        <h1>{{ $t("domains.page.title") }}</h1>
        <p class="page-desc">
          {{ $t("domains.page.descriptionPre") }}
          <span class="mono">gotham-traefik</span>. {{ $t("domains.page.descriptionPost") }}
        </p>
      </div>
      <div class="page-actions">
        <NButton @click="openProviderCreate">{{ $t("domains.page.addProvider") }}</NButton>
        <NButton type="primary" @click="openCertificateCreate">
          {{ $t("domains.page.addCertificate") }}
        </NButton>
      </div>
    </div>

    <NAlert
      v-if="proxyStore.error"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ proxyStore.error }}
    </NAlert>

    <DomainsStats />

    <NTabs v-model:value="activeTab" type="line" animated class="tabs">
      <NTabPane name="routers" :tab="$t('domains.tabs.routers')">
        <RoutersPanel />
      </NTabPane>

      <NTabPane name="certificates" :tab="$t('domains.tabs.certificates')">
        <CertificatesPanel />
      </NTabPane>

      <NTabPane name="dns" :tab="$t('domains.tabs.dns')">
        <DnsProvidersPanel />
      </NTabPane>

      <NTabPane name="redirects" :tab="$t('domains.tabs.redirects')">
        <RedirectRulesPanel />
        <RedirectCreateCard />
      </NTabPane>
    </NTabs>

    <ProviderDialog />
    <CertificateDialog />
    <RedirectEditDialog />
  </div>
</template>

<style scoped>
.domains-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.mono {
  font-family: var(--font-mono);
}

.tabs {
  margin-top: var(--space-2);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
