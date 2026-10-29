<script setup lang="ts">
import { NAlert } from "naive-ui";

import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  isDeploying: boolean;
}

defineProps<Props>();

function t(key: string): string {
  void activeLocale.value;
  return String(i18n.global.t(key));
}
</script>

<template>
  <NAlert v-if="isDeploying" type="info" :show-icon="true" data-testid="service-deploying-banner">
    {{ t("services.detail.deployingBanner") }}
  </NAlert>
</template>
