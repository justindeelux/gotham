<script setup lang="ts">
import StoragesPanel from "@/features/storages/components/StoragesPanel.vue";

import { i18n } from "@/shared/i18n";

function t(key: string): string {
  return String(i18n.global.t(key));
}
</script>

<template>
  <div class="storages-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("storages.page.eyebrow") }}</p>
        <h1>{{ t("storages.page.title") }}</h1>
        <p class="page-desc">{{ t("storages.page.description") }}</p>
      </div>
    </div>

    <StoragesPanel />
  </div>
</template>
