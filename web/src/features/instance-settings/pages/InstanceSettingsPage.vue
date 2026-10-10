<script setup lang="ts">
import { NAlert, NSpace, NSpin, NTabPane, NTabs } from "naive-ui";
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import GeneralForm from "@/features/instance-settings/components/GeneralForm.vue";
import NetworkConfirmCard from "@/features/instance-settings/components/NetworkConfirmCard.vue";
import NetworkForm from "@/features/instance-settings/components/NetworkForm.vue";
import SystemForm from "@/features/instance-settings/components/SystemForm.vue";
import { useInstanceSettings } from "@/features/instance-settings/composables/useInstanceSettings";

/**
 * Settings → Instance (JUS-92), ported from docs/design/instance-settings.html.
 * Platform operators only: a 403 renders a notice instead of the forms.
 */
const { t } = useI18n();
const { state, loading, forbidden, loadError, load, apply } = useInstanceSettings();
const pending = computed(() => state.value?.pending ?? null);

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="instance-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("instance-settings.page.eyebrow") }}</p>
        <h1>{{ t("instance-settings.page.title") }}</h1>
        <p class="page-desc">{{ t("instance-settings.page.description") }}</p>
      </div>
    </div>

    <NSpin v-if="loading && !state" />
    <NAlert v-else-if="forbidden" type="warning" :show-icon="true">
      {{ t("instance-settings.forbidden") }}
    </NAlert>
    <NAlert v-else-if="loadError" type="error" :show-icon="true">{{ loadError }}</NAlert>

    <NSpace v-else-if="state" vertical :size="16" class="instance-panels">
      <NetworkConfirmCard
        v-if="pending"
        :pending="pending"
        @settled="apply"
        @expired="load"
      />
      <NTabs type="line" animated>
        <NTabPane name="general" :tab="t('instance-settings.tabs.general')">
          <GeneralForm :state="state" @saved="apply" />
        </NTabPane>
        <NTabPane name="network" :tab="t('instance-settings.tabs.network')">
          <NetworkForm :state="state" @saved="apply" />
        </NTabPane>
        <NTabPane name="system" :tab="t('instance-settings.tabs.system')">
          <SystemForm :state="state" @saved="apply" />
        </NTabPane>
      </NTabs>
    </NSpace>
  </div>
</template>

<style scoped>
.instance-panels {
  max-width: 760px;
}
</style>
