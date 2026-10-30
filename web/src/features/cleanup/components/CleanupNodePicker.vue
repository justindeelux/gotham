<script setup lang="ts">
import {
  NCard,
  NCheckbox,
  NEmpty,
  NInputNumber,
  NSelect,
  NTag,
} from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { Server } from "@/features/servers/api/servers";

interface Props {
  servers: Server[];
  selected: Set<string>;
  minAgeValue: number;
  minAgeUnit: "hours" | "days";
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:selected": [value: Set<string>];
  "update:minAgeValue": [value: number];
  "update:minAgeUnit": [value: "hours" | "days"];
}>();

const { t } = useI18n();

const unitOptions = computed(() => [
  { label: t("cleanup.servers.hours"), value: "hours" as const },
  { label: t("cleanup.servers.days"), value: "days" as const },
]);

const maxAge = computed(() => (props.minAgeUnit === "days" ? 365 : 8760));

function toggle(serverId: string, checked: boolean): void {
  const next = new Set(props.selected);
  if (checked) {
    next.add(serverId);
  } else {
    next.delete(serverId);
  }
  emit("update:selected", next);
}

function updateAge(value: number | null): void {
  emit("update:minAgeValue", value ?? 0);
}
</script>

<template>
  <NCard :title="t('cleanup.servers.title')" class="cleanup-card">
    <NEmpty v-if="servers.length === 0" :description="t('cleanup.servers.empty')" />

    <template v-else>
      <p class="cleanup-hint">{{ t("cleanup.servers.hint") }}</p>

      <ul class="cleanup-node-list">
        <li v-for="server in servers" :key="server.id" class="cleanup-node">
          <NCheckbox
            :checked="selected.has(server.id)"
            :disabled="server.status !== 'ready'"
            @update:checked="(checked: boolean) => toggle(server.id, checked)"
          >
            <span class="cleanup-node__name">{{ server.name }}</span>
          </NCheckbox>
          <span class="cleanup-node__meta">{{ server.ip }}</span>
          <NTag v-if="server.status !== 'ready'" size="small" type="default">
            {{ t("cleanup.servers.offline") }}
          </NTag>
        </li>
      </ul>

      <div class="cleanup-age">
        <span class="cleanup-age__label">{{ t("cleanup.servers.minAge") }}</span>
        <NInputNumber
          :value="minAgeValue"
          :min="0"
          :max="maxAge"
          :input-props="{ 'aria-label': t('cleanup.servers.minAge') }"
          class="cleanup-age__value"
          @update:value="updateAge"
        />
        <NSelect
          :value="minAgeUnit"
          :options="unitOptions"
          class="cleanup-age__unit"
          @update:value="emit('update:minAgeUnit', $event)"
        />
      </div>
      <p class="cleanup-hint">{{ t("cleanup.servers.minAgeHint") }}</p>
    </template>
  </NCard>
</template>

<style scoped>
.cleanup-hint {
  margin: 0 0 var(--space-3);
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-node-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0 0 var(--space-4);
  padding: 0;
  list-style: none;
}

.cleanup-node {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
  min-width: 0;
}

.cleanup-node__name {
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-node__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--meta);
}

.cleanup-age {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.cleanup-age__label {
  font-size: var(--text-sm);
  color: var(--fg);
}

.cleanup-age__value {
  width: 120px;
}

.cleanup-age__unit {
  width: 120px;
}

@media (max-width: 480px) {
  .cleanup-age__value,
  .cleanup-age__unit {
    width: 100%;
  }
}
</style>
