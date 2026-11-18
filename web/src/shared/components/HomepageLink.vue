<script setup lang="ts">
import { ExternalLink } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

const props = defineProps<{ href?: string; name?: string }>();

const { t } = useI18n();

const label = computed<string>(() => {
  const open = String(t("common.actions.openWebsite"));
  return props.name ? `${open}: ${props.name}` : open;
});
</script>

<template>
  <NButton
    v-if="href"
    tag="a"
    :href="href"
    target="_blank"
    rel="noopener noreferrer"
    :aria-label="label"
    size="small"
    quaternary
  >
    <template #icon>
      <ExternalLink :size="14" :stroke-width="1.8" aria-hidden="true" />
    </template>
    {{ t("common.actions.openWebsite") }}
  </NButton>
</template>
