<script setup lang="ts">
import { NAlert, NButton, NCard, NSpin } from "naive-ui";
import { useI18n } from "vue-i18n";

import TerminalPane from "@/features/terminal/components/TerminalPane.vue";
import type { Server } from "@/features/servers";

/**
 * Host and state view for one page session (JUS-246).
 *
 * The stage never starts a session by itself: `start` is emitted only from an
 * explicit button (closed session, or the moved-to-window notice), so a route
 * change, a close or a demotion can never silently spawn a shell.
 */
interface Props {
  server: Server | null;
  ready: boolean;
  tabId: string;
  starting: boolean;
  closed: boolean;
}

defineProps<Props>();
const emit = defineEmits<{ start: [fresh: boolean] }>();
const { t } = useI18n();

/** statusLabel localizes the server lifecycle word. */
function statusLabel(server: Server): string {
  return t(`servers.status.${server.status}`);
}
</script>

<template>
  <div class="term-stage">
    <NAlert
      v-if="server === null"
      type="error"
      :show-icon="true"
      class="term-stage-alert"
    >
      {{ t("terminal.page.unknownServer") }}
    </NAlert>
    <NAlert
      v-else-if="!ready"
      type="warning"
      :show-icon="true"
      class="term-stage-alert"
    >
      <template #header>{{ t("terminal.page.notReadyTitle") }}</template>
      {{
        t("terminal.page.notReadyBody", {
          status: statusLabel(server),
        })
      }}
    </NAlert>
    <TerminalPane
      v-else-if="tabId !== ''"
      :tab-id="tabId"
      class="term-stage-pane"
      @start-new="emit('start', true)"
    />
    <div v-else-if="starting" class="term-stage-loading">
      <NSpin size="small" :aria-label="t('terminal.page.starting')" />
      <p class="small muted">{{ t("terminal.page.starting") }}</p>
    </div>
    <NCard v-else class="term-stage-closed">
      <p class="term-closed-title">
        {{ t("terminal.page.sessionClosedTitle") }}
      </p>
      <p class="small muted term-closed-body">
        {{ t("terminal.page.sessionClosedBody") }}
      </p>
      <div class="term-closed-actions">
        <NButton type="primary" @click="emit('start', true)">
          {{ t("terminal.page.startNew") }}
        </NButton>
      </div>
    </NCard>
  </div>
</template>

<style scoped>
.term-stage {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.term-stage-alert {
  flex: 0 0 auto;
}

.term-stage-loading {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
}

.term-stage-pane {
  flex: 1 1 auto;
  min-height: 0;
}

.term-stage-closed {
  flex: 0 0 auto;
}

.term-closed-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.term-closed-body {
  margin: var(--space-2) 0 var(--space-4);
  max-width: 60ch;
}

.term-closed-actions {
  display: flex;
  gap: var(--space-2);
}
</style>
