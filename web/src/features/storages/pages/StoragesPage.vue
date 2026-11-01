<script setup lang="ts">
import { NEmpty } from "naive-ui";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
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

    <NEmpty :description="t('storages.page.empty')" />
  </div>
</template>
