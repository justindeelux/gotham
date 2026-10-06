<script setup lang="ts">
import { NSelect } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed } from "vue";

import { activeLocale, setLocale } from "@/shared/i18n";
import type { Locale } from "@/shared/i18n";

/** Options use autonyms, never translated labels. */
const options: SelectOption[] = [
  { value: "en", label: "English" },
  { value: "vi", label: "Tiếng Việt" },
];

/** value bridges the Naive select model to the locale setter. */
const value = computed<Locale>({
  get: () => activeLocale.value,
  set: (locale: Locale) => {
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
    aria-label="Language"
    class="language-select"
  />
</template>

<style scoped>
.language-select {
  width: 132px;
}
</style>
