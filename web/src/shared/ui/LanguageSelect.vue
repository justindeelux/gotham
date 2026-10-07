<script setup lang="ts">
import { NButton, NDropdown } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { activeLocale, setLocale } from "@/shared/i18n";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

const { t } = useI18n();

const show = ref(false);
const button = ref<InstanceType<typeof NButton> | null>(null);
const options = computed<DropdownOption[]>(() =>
  ["en", "vi"].map((locale) => ({
    label: t(`language.names.${locale}`),
    key: locale,
    props: { role: "menuitemradio", "aria-checked": activeLocale.value === locale },
  })),
);

function handleSelect(locale: string | number): void {
  setLocale(locale);
  (button.value?.$el as HTMLButtonElement | undefined)?.focus();
}
</script>

<template>
  <NDropdown
    v-model:show="show"
    trigger="click"
    :options="options"
    :value="activeLocale"
    :menu-props="() => ({ role: 'menu', 'aria-label': t('language.label') })"
    @select="handleSelect"
  >
    <NButton
      ref="button"
      quaternary
      circle
      class="language-select"
      :aria-label="t('language.label')"
      aria-haspopup="menu"
      :aria-expanded="show"
    >
      <template #icon>
        <GothamIcon name="globe" />
      </template>
    </NButton>
  </NDropdown>
</template>
