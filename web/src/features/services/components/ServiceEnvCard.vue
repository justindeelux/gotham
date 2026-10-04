<script setup lang="ts">
import { NAlert, NButton, NCard, NInput, NSpace, NText } from "naive-ui";

import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";

/** ServiceEnvCard renders the masked environment editor card. */
const {
  error,
  envReference,
  envDraft,
  envSaving,
  envError,
  canEditCurrent,
  addEnvRow,
  updateEnvRow,
  removeEnvRow,
  handleSaveEnv,
} = useServiceDetailContext();
</script>

<template>
  <NCard title="Environment">
    <template #header-extra>
      <NText depth="3" class="small">
        <span class="mono">{{ envReference }}</span> substitution input
      </NText>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="envError" type="error" :show-icon="true">
        {{ envError }}
      </NAlert>
      <template v-if="canEditCurrent">
        <p class="small muted">
          Values are masked here: a service created from a template keeps
          its secret values in this map, and they must never be displayed in
          clear. The control plane redacts every value from errors and from
          the deploy history.
        </p>
        <div v-if="envDraft.length > 0" class="env-rows">
          <div v-for="row in envDraft" :key="row.id" class="env-row">
            <NInput
              :value="row.key"
              class="mono"
              placeholder="MYSQL_PASSWORD"
              aria-label="Variable name"
              @update:value="(value: string) => updateEnvRow(row.id, { key: value })"
            />
            <NInput
              :value="row.value"
              type="password"
              show-password-on="click"
              :input-props="{ autocomplete: 'new-password' }"
              class="mono"
              placeholder="value"
              aria-label="Variable value"
              @update:value="(value: string) => updateEnvRow(row.id, { value })"
            />
            <NButton
              quaternary
              type="error"
              aria-label="Remove variable"
              @click="removeEnvRow(row.id)"
            >
              Remove
            </NButton>
          </div>
        </div>
        <NText v-else depth="3">
          The environment is empty. Saving an empty environment clears it.
        </NText>
        <NSpace :size="8" align="center">
          <NButton size="small" @click="addEnvRow">Add variable</NButton>
          <NButton
            size="small"
            type="primary"
            :loading="envSaving"
            @click="handleSaveEnv"
          >
            Save environment
          </NButton>
        </NSpace>
      </template>
      <NText v-else depth="3" class="small">
        {{
          error
            ? "The environment is unavailable."
            : "Loading the environment…"
        }}
      </NText>
    </NSpace>
  </NCard>
</template>

<style scoped>
.env-rows {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.env-row {
  display: grid;
  grid-template-columns: minmax(0, 220px) minmax(0, 1fr) auto;
  gap: var(--space-2);
  align-items: center;
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 860px) {
  .env-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
