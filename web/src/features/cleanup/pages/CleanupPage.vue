<script setup lang="ts">
import { NTabPane, NTabs } from "naive-ui";
import { ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import CleanupExclusionsTab from "@/features/cleanup/components/CleanupExclusionsTab.vue";
import CleanupHistoryTab from "@/features/cleanup/components/CleanupHistoryTab.vue";
import CleanupScanTab from "@/features/cleanup/components/CleanupScanTab.vue";
import CleanupSchedulesTab from "@/features/cleanup/components/CleanupSchedulesTab.vue";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

type CleanupTab = "scan" | "schedules" | "history" | "exclusions";

const tabNames: CleanupTab[] = ["scan", "schedules", "history", "exclusions"];

function tabFromQuery(): CleanupTab {
  const raw = route.query.tab;
  return tabNames.find((name) => name === raw) ?? "scan";
}

const activeTab = ref<CleanupTab>(tabFromQuery());

watch(
  () => route.query.tab,
  () => {
    activeTab.value = tabFromQuery();
  },
);

watch(activeTab, (value) => {
  if (route.query.tab === value) {
    return;
  }
  void router.replace({ query: { ...route.query, tab: value } });
});
</script>

<template>
  <div class="cleanup-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("cleanup.page.eyebrow") }}</p>
        <h1>{{ t("cleanup.page.title") }}</h1>
        <p class="page-desc">{{ t("cleanup.page.description") }}</p>
      </div>
    </div>

    <NTabs v-model:value="activeTab" type="line" animated display-directive="show:lazy">
      <NTabPane name="scan" :tab="t('cleanup.tabs.scan')">
        <CleanupScanTab />
      </NTabPane>
      <NTabPane name="schedules" :tab="t('cleanup.tabs.schedules')">
        <CleanupSchedulesTab />
      </NTabPane>
      <NTabPane name="history" :tab="t('cleanup.tabs.history')">
        <CleanupHistoryTab />
      </NTabPane>
      <NTabPane name="exclusions" :tab="t('cleanup.tabs.exclusions')">
        <CleanupExclusionsTab />
      </NTabPane>
    </NTabs>
  </div>
</template>

<style scoped>
.cleanup-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
</style>
