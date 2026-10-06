<script setup lang="ts">
import { NSelect } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { activeLocale, setLocale } from "@/shared/i18n";
import type { Locale } from "@/shared/i18n";

const { t } = useI18n();

/** Options use autonyms from the catalog, never translated labels. */
const options = computed<SelectOption[]>(() => [
  { value: "en", label: t("language.names.en") },
  { value: "vi", label: t("language.names.vi") },
]);

/** value bridges the Naive select model to the locale setter. */
const value = computed<Locale>({
  get: () => activeLocale.value,
  set: (locale: string) => {
    setLocale(locale);
  },
});
</script>

<template>
  <NSelect
    v-model:value="value"
    :options="options"
    :consistent-menu-width="false"
    size="small"
    :aria-label="t('language.label')"
    class="language-select"
  />
</template>

<style scoped>
.language-select {
  width: 132px;
}
</style>
