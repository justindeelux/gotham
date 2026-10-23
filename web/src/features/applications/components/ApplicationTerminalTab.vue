<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NSpin } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import ApplicationTerminalTargetPicker from "@/features/applications/components/ApplicationTerminalTargetPicker.vue";
import { useApplicationTerminalTab } from "@/features/applications/composables/useApplicationTerminalTab";
import TerminalSessionStage from "@/features/terminal/components/TerminalSessionStage.vue";
import { errorKey } from "@/features/terminal/utils/terminal";

interface Props {
  applicationId: string;
  applicationName: string;
  accessKnown: boolean;
  accessError: boolean;
  canWrite: boolean;
  active: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{ retryAccess: [] }>();
const { t } = useI18n();

const canUse = computed<boolean>(() => props.accessKnown && props.canWrite);

const {
  targetOptions,
  targetsLoading,
  targetsError,
  mode,
  selection,
  tabId,
  starting,
  closed,
  showPicker,
  refresh,
  retry,
  startNew,
  selectTarget,
} = useApplicationTerminalTab({
  applicationId: () => props.applicationId,
  applicationName: () => props.applicationName,
  canUse: () => canUse.value,
  active: () => props.active,
});

const errorText = computed<string>(() => t(errorKey(targetsError.value)));
</script>

<template>
  <div class="app-term">
    <NAlert
      v-if="!accessKnown && accessError"
      type="error"
      :show-icon="true"
      class="app-term-alert app-term-access-error"
    >
      <template #header>
        {{ t("applications.terminal.accessErrorTitle") }}
      </template>
      <p class="app-term-copy">
        {{ t("applications.terminal.accessErrorBody") }}
      </p>
      <div class="app-term-actions">
        <NButton class="app-term-access-retry" type="primary" @click="emit('retryAccess')">
          {{ t("applications.terminal.retry") }}
        </NButton>
      </div>
    </NAlert>

    <div
      v-else-if="!accessKnown"
      class="app-term-checking"
      role="status"
      aria-live="polite"
    >
      <NSpin size="small" :aria-label="t('applications.terminal.checkingAccess')" />
      <p class="small muted">{{ t("applications.terminal.checkingAccess") }}</p>
    </div>

    <template v-else>
      <ApplicationTerminalTargetPicker
        v-if="mode === 'session' && showPicker"
        :options="targetOptions"
        :value="selection"
        :loading="targetsLoading"
        :disabled="starting"
        :label="t('applications.terminal.serviceLabel')"
        @select="selectTarget"
        @refresh="refresh"
      />
      <div v-else-if="mode === 'session'" class="app-term-toolbar">
        <NButton
          secondary
          class="app-term-refresh"
          :loading="targetsLoading"
          @click="refresh"
        >
          {{ t("applications.terminal.refresh") }}
        </NButton>
      </div>

      <NAlert
        v-if="mode === 'error'"
        type="error"
        :show-icon="true"
        class="app-term-alert"
      >
        <template #header>{{ t("terminal.errors.title") }}</template>
        <p class="app-term-copy">{{ errorText }}</p>
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
        <ApplicationTerminalTargetPicker
          :options="targetOptions"
          :value="selection"
          :loading="targetsLoading"
          :disabled="starting"
          @select="selectTarget"
          @refresh="refresh"
        />
      </NCard>

      <TerminalSessionStage
        v-else-if="mode === 'session'"
        mode="application"
        :server="null"
        :ready="true"
        :tab-id="tabId"
        :starting="starting"
        :closed="closed"
        @start="startNew"
      />
    </template>
  </div>
</template>

<style scoped>
.app-term {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
}

.app-term-toolbar {
  display: flex;
  justify-content: flex-end;
}

.app-term-checking,
.app-term-loading {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-4);
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

.app-term-select-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.app-term-empty-title {
  margin: 0 0 var(--space-2);
  font-weight: 600;
  color: var(--fg-2);
}
</style>
