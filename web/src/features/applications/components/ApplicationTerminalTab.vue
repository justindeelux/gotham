<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NSelect, NSpin } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useApplicationTerminalTab } from "@/features/applications/composables/useApplicationTerminalTab";
import TerminalSessionStage from "@/features/terminal/components/TerminalSessionStage.vue";
import { errorKey } from "@/features/terminal/utils/terminal";

/**
 * Application detail Terminal tab (JUS-247).
 *
 * The tab is a thin host: `useApplicationTerminalTab` owns lazy target
 * discovery, the single-target auto-open, the explicit multi-service selector
 * and the shared close/restart lifecycle. The session itself is the same
 * TerminalSessionStage/TerminalPane the server tab uses, so the buffer, dock,
 * rail count, close confirmation and one-way window transfer are shared, never
 * forked. Host and container sessions never share a create path.
 */
interface Props {
  applicationId: string;
  applicationName: string;
  canWrite: boolean;
  active: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{ blocked: [] }>();
const { t } = useI18n();

const {
  targets,
  targetOptions,
  targetsLoading,
  targetsError,
  mode,
  selection,
  tabId,
  starting,
  closed,
  refresh,
  retry,
  selectTarget,
  startNew,
} = useApplicationTerminalTab({
  applicationId: () => props.applicationId,
  applicationName: () => props.applicationName,
  canWrite: () => props.canWrite,
  active: () => props.active,
  onBlocked: () => emit("blocked"),
});

/** errorText is bounded catalog copy; the backend body is never rendered. */
function errorText(): string {
  return t(errorKey(targetsError.value));
}
</script>

<template>
  <div class="app-term">
    <div
      v-if="targets.length > 1 && mode !== 'select'"
      class="app-term-switch"
    >
      <label class="app-term-label" for="app-term-target">
        {{ t("applications.terminal.serviceLabel") }}
      </label>
      <div class="app-term-switch-row">
        <NSelect
          id="app-term-target"
          class="app-term-select"
          :value="selection"
          :options="targetOptions"
          :placeholder="t('applications.terminal.chooseService')"
          :loading="targetsLoading"
          :disabled="starting"
          @update:value="selectTarget"
        />
        <NButton
          secondary
          class="app-term-refresh"
          :loading="targetsLoading"
          @click="refresh"
        >
          {{ t("applications.terminal.refresh") }}
        </NButton>
      </div>
    </div>

    <NAlert
      v-if="mode === 'error'"
      type="error"
      :show-icon="true"
      class="app-term-alert"
    >
      <template #header>{{ t("terminal.errors.title") }}</template>
      <p class="app-term-copy">{{ errorText() }}</p>
      <div class="app-term-actions">
        <NButton class="app-term-retry" type="primary" @click="retry">
          {{ t("applications.terminal.retry") }}
        </NButton>
      </div>
    </NAlert>

    <NCard
      v-else-if="mode === 'empty'"
      class="app-term-empty"
      role="status"
      aria-live="polite"
    >
      <p class="app-term-empty-title">
        {{ t("applications.terminal.emptyTitle") }}
      </p>
      <NEmpty :description="t('applications.terminal.emptyBody')">
        <template #extra>
          <NButton secondary class="app-term-refresh" @click="refresh">
            {{ t("applications.terminal.refresh") }}
          </NButton>
        </template>
      </NEmpty>
    </NCard>

    <div
      v-else-if="mode === 'loading'"
      class="app-term-loading"
      role="status"
      aria-live="polite"
    >
      <NSpin size="small" :aria-label="t('applications.terminal.loading')" />
      <p class="small muted">{{ t("applications.terminal.loading") }}</p>
    </div>

    <NCard
      v-else-if="mode === 'select'"
      class="app-term-select-card"
      role="status"
      aria-live="polite"
    >
      <p class="app-term-select-title">
        {{ t("applications.terminal.selectTitle") }}
      </p>
      <p class="small muted app-term-copy">
        {{ t("applications.terminal.selectBody") }}
      </p>
      <div class="app-term-switch-row">
        <NSelect
          class="app-term-select"
          :value="selection"
          :options="targetOptions"
          :placeholder="t('applications.terminal.chooseService')"
          :loading="targetsLoading"
          :disabled="starting"
          @update:value="selectTarget"
        />
        <NButton
          secondary
          class="app-term-refresh"
          :loading="targetsLoading"
          @click="refresh"
        >
          {{ t("applications.terminal.refresh") }}
        </NButton>
      </div>
    </NCard>

    <TerminalSessionStage
      v-else
      mode="application"
      :server="null"
      :ready="true"
      :tab-id="tabId"
      :starting="starting"
      :closed="closed"
      @start="startNew"
    />
  </div>
</template>

<style scoped>
.app-term {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
}

.app-term-switch {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}

.app-term-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.app-term-switch-row {
  display: flex;
  flex: 1 1 18rem;
  gap: var(--space-2);
  min-width: 0;
}

.app-term-select {
  min-width: 0;
  flex: 1 1 auto;
}

.app-term-alert,
.app-term-empty,
.app-term-select-card {
  max-width: 60ch;
}

.app-term-copy {
  margin: var(--space-2) 0;
}

.app-term-actions {
  display: flex;
  gap: var(--space-2);
}

.app-term-loading {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-4);
}

.app-term-select-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.app-term-empty-title {
  font-weight: 600;
  color: var(--fg-2);
}

@media (max-width: 640px) {
  .app-term-switch-row {
    flex-direction: column;
  }
}
</style>
