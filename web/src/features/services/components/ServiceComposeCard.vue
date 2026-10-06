<script setup lang="ts">
import { NAlert, NCard, NSpace, NText } from "naive-ui";

import ComposeEditor from "@/features/services/components/ComposeEditor.vue";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";

/** ServiceComposeCard renders the compose.yaml view/edit card. */
/**
 * t renders card copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without losing drafts.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const {
  service,
  error,
  composeYaml,
  composeEditing,
  composeSaving,
  composeError,
  canEditCurrent,
  handleSaveCompose,
} = useServiceDetailContext();
</script>

<template>
  <NCard :title="t('services.compose.title')">
    <template #header-extra>
      <NText depth="3" class="small">
        {{ t("services.compose.savedNote") }}
      </NText>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="composeError" type="error" :show-icon="true">
        {{ composeError }}
      </NAlert>
      <ComposeEditor
        v-if="canEditCurrent"
        v-model="composeYaml"
        v-model:editing="composeEditing"
        :saving="composeSaving"
        :label="`compose_yaml · ${service?.name}`"
        @save="handleSaveCompose"
      />
      <NText v-else depth="3" class="small">
        {{
          error
            ? t("services.compose.unavailable")
            : t("services.compose.loading")
        }}
      </NText>
    </NSpace>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}
</style>
