<script setup lang="ts">
import { NAlert, NCard, NSpace, NText } from "naive-ui";

import ComposeEditor from "@/features/services/components/ComposeEditor.vue";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";

/** ServiceComposeCard renders the compose.yaml view/edit card. */
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
  <NCard title="compose.yaml">
    <template #header-extra>
      <NText depth="3" class="small">
        saving creates a new version; the running project switches on deploy
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
            ? "The compose document is unavailable."
            : "Loading the service configuration…"
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
