<script setup lang="ts">
import { NRadioButton, NRadioGroup } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { activeLocale, setLocale } from "@/shared/i18n";

const { t } = useI18n();

/** value bridges the Naive radio model to the locale setter. */
const value = computed<string>({
  get: () => activeLocale.value,
  set: (locale: string) => {
    setLocale(locale);
  },
});
</script>

<template>
  <div
    class="language-select"
    role="radiogroup"
    :aria-label="t('language.label')"
  >
    <NRadioGroup v-model:value="value" size="small">
      <NRadioButton value="en">
        {{ t("language.names.en") }}
      </NRadioButton>
      <NRadioButton value="vi">
        {{ t("language.names.vi") }}
      </NRadioButton>
    </NRadioGroup>
  </div>
</template>

<style scoped>
.language-select {
  display: inline-flex;
}
</style>
