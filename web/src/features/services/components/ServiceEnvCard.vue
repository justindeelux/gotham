<script setup lang="ts">
import { NAlert, NButton, NCard, NInput, NSpace, NText } from "naive-ui";

import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";

/** ServiceEnvCard renders the masked environment editor card. */

/**
 * t renders card copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without losing env rows.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

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
  <NCard :title="t('services.env.title')">
    <template #header-extra>
      <NText depth="3" class="small">
        {{ t("services.env.substitutionInput", { ref: envReference }) }}
      </NText>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="envError" type="error" :show-icon="true">
        {{ envError }}
      </NAlert>
      <template v-if="canEditCurrent">
        <p class="small muted">
          {{ t("services.env.maskedNote") }}
        </p>
        <div v-if="envDraft.length > 0" class="env-rows">
          <div v-for="row in envDraft" :key="row.id" class="env-row">
            <NInput
              :value="row.key"
              class="mono"
              :placeholder="t('services.env.keyPlaceholder')"
              :aria-label="t('services.env.keyAria')"
              @update:value="(value: string) => updateEnvRow(row.id, { key: value })"
            />
            <NInput
              :value="row.value"
              type="password"
              show-password-on="click"
              :input-props="{ autocomplete: 'new-password' }"
              class="mono"
              :placeholder="t('services.env.valuePlaceholder')"
              :aria-label="t('services.env.valueAria')"
              @update:value="(value: string) => updateEnvRow(row.id, { value })"
            />
            <NButton
              quaternary
              type="error"
              :aria-label="t('services.env.remove')"
              @click="removeEnvRow(row.id)"
            >
              {{ t("services.env.remove") }}
            </NButton>
          </div>
        </div>
        <NText v-else depth="3">
          {{ t("services.env.empty") }}
        </NText>
        <NSpace :size="8" align="center">
          <NButton size="small" @click="addEnvRow">{{ t("services.env.add") }}</NButton>
          <NButton
            size="small"
            type="primary"
            :loading="envSaving"
            @click="handleSaveEnv"
          >
            {{ t("services.env.save") }}
          </NButton>
        </NSpace>
      </template>
      <NText v-else depth="3" class="small">
        {{
          error
            ? t("services.env.unavailable")
            : t("services.env.loading")
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
