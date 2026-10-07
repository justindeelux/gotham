<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NTag, NText } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { EnvVar } from "@/features/applications/api/applications";
import EnvEditor from "@/features/applications/components/EnvEditor.vue";
import {
  inheritedOriginLabel,
  isOverriddenBy,
  isShadowedByEnvironment,
} from "@/features/projects/schemas/variables";
import type { InheritedVariable } from "@/features/projects/schemas/variables";

interface Props {
  envDraft: EnvVar[];
  envLoading: boolean;
  envError: string | null;
  saveDisabled: boolean;
  saving: boolean;
  /** inherited lists the project/environment rows shown read-only, if any. */
  inherited?: InheritedVariable[];
  inheritedLoading?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  inherited: () => [],
  inheritedLoading: false,
});

const emit = defineEmits<{
  "update:draft": [value: EnvVar[]];
  save: [];
  retry: [];
}>();

const { t } = useI18n();

/**
 * isRowOverridden marks a row the application draft shadows, and — for
 * project rows — a key the environment set already shadows. Deploy
 * precedence is project < environment < application, so a project row with
 * the same key in the environment set never takes effect.
 */
function isRowOverridden(row: InheritedVariable): boolean {
  if (isOverriddenBy(row.key, props.envDraft)) {
    return true;
  }
  return (
    row.origin === "project" &&
    isShadowedByEnvironment(row.key, props.inherited)
  );
}
</script>

<template>
  <NCard style="margin-top: 16px" :title="t('applications.envTab.title')">
    <template #header-extra>
      <NButton
        type="primary"
        size="small"
        :loading="props.saving"
        :disabled="props.saveDisabled"
        @click="emit('save')"
      >
        {{ t("common.actions.save") }}
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert
        v-if="props.envError"
        type="error"
        :show-icon="true"
      >
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.envError }}</span>
          <NButton size="small" @click="emit('retry')">{{ t("common.actions.retry") }}</NButton>
        </NSpace>
      </NAlert>
      <NSpin :show="props.envLoading">
        <div
          v-if="props.inheritedLoading || props.inherited.length > 0"
          class="inherited"
        >
          <NText depth="3" class="inherited-title">
            {{ t("applications.envTab.inheritedTitle") }}
          </NText>
          <NText depth="3">
            {{ t("applications.envTab.inheritedHint") }}
          </NText>
          <div v-if="!props.inheritedLoading" class="table-wrap">
            <table class="inherited-table">
              <thead>
                <tr>
                  <th scope="col">{{ t("applications.envTab.key") }}</th>
                  <th scope="col">{{ t("applications.envTab.value") }}</th>
                  <th scope="col">{{ t("applications.envTab.origin") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in props.inherited"
                  :key="`${row.origin}:${row.key}`"
                >
                  <td class="mono" :data-label="t('applications.envTab.key')">
                    {{ row.key }}
                    <NTag v-if="row.secret" size="small">{{ t("applications.envTab.secret") }}</NTag>
                  </td>
                  <td class="mono muted" :data-label="t('applications.envTab.value')">
                    {{ row.secret ? "••••••••" : row.value }}
                  </td>
                  <td :data-label="t('applications.envTab.origin')">
                    <NSpace :size="4" align="center" wrap>
                      <NTag size="small">{{ inheritedOriginLabel(row.origin) }}</NTag>
                      <NTag
                        v-if="isRowOverridden(row)"
                        size="small"
                        type="warning"
                      >
                        {{ t("applications.envTab.overridden") }}
                      </NTag>
                    </NSpace>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <EnvEditor :model-value="props.envDraft" @update:model-value="emit('update:draft', $event)" />
      </NSpin>
    </NSpace>
    <template #footer>
      <NText depth="3">
        {{ t("applications.envTab.footer") }}
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.inherited {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
  container-type: inline-size;
}

.inherited-title {
  font-weight: 600;
}

.table-wrap {
  overflow-x: auto;
}

.inherited-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-sm);
}

.inherited-table th,
.inherited-table td {
  text-align: left;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.inherited-table th {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.muted {
  color: var(--muted);
}

@container (max-width: 560px) {
  .inherited-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }

  .inherited-table,
  .inherited-table tbody,
  .inherited-table tr,
  .inherited-table td {
    display: block;
    width: auto;
    max-width: none;
  }

  .inherited-table tr {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }

  .inherited-table td {
    border-bottom: 0;
    padding: 4px 0;
    white-space: normal;
    overflow: visible;
    text-overflow: clip;
  }
}
</style>
