<script setup lang="ts">
import { NAlert, NSpin } from "naive-ui";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import AuthenticationSection from "@/features/instance-settings/components/AuthenticationSection.vue";
import { useInstanceSettings } from "@/features/instance-settings/composables/useInstanceSettings";

const { t } = useI18n();
const { state, loading, forbidden, loadError, load } = useInstanceSettings();

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="authentication-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("instance-settings.oauth.eyebrow") }}</p>
        <h1>{{ t("instance-settings.oauth.title") }}</h1>
        <p class="page-desc">{{ t("instance-settings.oauth.description") }}</p>
      </div>
    </div>

    <NSpin v-if="loading && !state" />
    <NAlert v-else-if="forbidden" type="warning" :show-icon="true">
      {{ t("instance-settings.forbidden") }}
    </NAlert>
    <NAlert v-else-if="loadError" type="error" :show-icon="true">{{ loadError }}</NAlert>

    <AuthenticationSection
      v-else-if="state"
      :control-plane-url="state.general.control_plane_url.value"
    />
  </div>
</template>

<style scoped>
.authentication-page {
  max-width: 760px;
}
</style>
