<script setup lang="ts">
import { NAlert, NSpin } from "naive-ui";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import GeneralForm from "@/features/instance-settings/components/GeneralForm.vue";
import { useInstanceSettings } from "@/features/instance-settings/composables/useInstanceSettings";

const { t } = useI18n();
const { state, loading, forbidden, loadError, load, apply } = useInstanceSettings();

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

    <GeneralForm v-else-if="state" :state="state" @saved="apply" />
  </div>
</template>

<style scoped>
.instance-page {
  max-width: 760px;
}
</style>
