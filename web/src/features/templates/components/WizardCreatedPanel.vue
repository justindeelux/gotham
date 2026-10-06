<script setup lang="ts">
import { NAlert, NButton, NSpace, NTag } from "naive-ui";
import { RouterLink } from "vue-router";

import type { Service } from "@/features/services";
import { serviceStatusLabel } from "@/features/services";
import ServiceLogs from "@/features/services/components/ServiceLogs.vue";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  created: Service;
  deployed: boolean;
  deployError: string | null;
  logServices: string[];
  logTitle: string;
}

defineProps<Props>();

const emit = defineEmits<{
  close: [];
}>();

/**
 * t renders success-panel copy in the active locale (tracks language
 * switches). Names, statuses and routes stay raw parameters.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}
</script>

<template>
  <div class="wizard__success" data-testid="wizard-created">
    <h4>{{ t("templates.created.title", { name: created.name }) }}</h4>
    <NSpace :size="8" align="center">
      <NTag size="small" type="warning">{{ serviceStatusLabel(created.status) }}</NTag>
      <NTag size="small" class="mono">{{ created.compose_project }}</NTag>
      <NTag
        v-for="route in created.domains"
        :key="route.domain"
        size="small"
        class="mono"
      >
        {{ route.domain }}
      </NTag>
    </NSpace>
    <p class="wizard__desc">
      {{ t("templates.created.description") }}
    </p>
    <NAlert v-if="deployError" type="error" :show-icon="true">
      {{ deployError }}
    </NAlert>
    <NSpace :size="8" align="center">
      <RouterLink
        :to="{
          name: 'service-detail',
          params: {
            projectId: created.project_id,
            environmentId: created.environment_id,
            id: created.id,
          },
        }"
        @click="emit('close')"
      >
        <NButton size="small">{{ t("templates.created.open") }}</NButton>
      </RouterLink>
      <NTag v-if="deployed" size="small" type="success">{{ t("templates.created.deployed") }}</NTag>
    </NSpace>
    <div class="wizard__stub">
      <h5>{{ t("templates.created.stubTitle") }}</h5>
      <p>
        {{ t("templates.created.stubBody") }}
      </p>
    </div>
    <ServiceLogs
      v-if="deployed"
      :service-id="created.id"
      :services="logServices"
      :title="logTitle"
    />
  </div>
</template>

<style scoped>
.wizard__success {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard__success h4 {
  font-size: var(--text-base);
}

.wizard__desc {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.wizard__stub {
  border-left: 4px solid var(--warn);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.wizard__stub h5 {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.wizard__stub p {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}
</style>
