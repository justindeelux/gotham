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

const proxyStore = useProxyStore();
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

.mono {
  font-family: var(--font-mono);
}

.tabs {
  margin-top: var(--space-2);
}
</style>
