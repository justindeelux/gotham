<script setup lang="ts">
import { NAlert, NButton, NCard, NSpin } from "naive-ui";
import { nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { Server } from "@/features/servers";
import { serverStatusKey } from "@/features/servers";
import TerminalPane from "@/features/terminal/components/TerminalPane.vue";

/**
 * Host and state view for one page session (JUS-246).
 *
 * The stage never starts a session by itself: `start` is emitted only from an
 * explicit button (closed session, or the moved-to-window notice), so a route
 * change, a close or a demotion can never silently spawn a shell. The closed
 * card is a polite live region and takes focus after a confirmed close, so the
 * keyboard flow continues on "Start new session" rather than on a rail tile.
 */
interface Props {
  server: Server | null;
  ready: boolean;
  tabId: string;
  starting: boolean;
  closed: boolean;
  /**
   * mode keeps the host readiness copy off the application tab (JUS-247): a
   * container session has no server lifecycle branch, it is only ready once
   * the control plane created it. Discovery/empty/error states belong to the
   * calling host; this stage renders the shared pane, loading and restart.
   */
  mode?: "host" | "application";
}

const props = withDefaults(defineProps<Props>(), { mode: "host" });
const emit = defineEmits<{ start: [fresh: boolean] }>();
const { t } = useI18n();

const startButton = ref<InstanceType<typeof NButton> | null>(null);

/** focusStart moves focus onto the explicit restart control. */
function focusStart(): void {
  const element = startButton.value?.$el;
  if (element instanceof HTMLElement) {
    element.focus();
  }
}

// A confirmed close removes the tab; the dialog restores focus to its own
// fallback on the next tick, so wait two ticks and land on the restart button.
watch(
  () => props.closed,
  async (closed) => {
    if (!closed) {
      return;
    }
    await nextTick();
    await nextTick();
    focusStart();
  },
);

/** statusLabel localizes the server lifecycle word. */
function statusLabel(server: Server): string {
  return t(serverStatusKey(server.status));
}
</script>

<template>
  <div class="term-stage">
    <NAlert
      v-if="mode === 'host' && server === null"
      type="error"
      :show-icon="true"
      class="term-stage-alert"
    >
      {{ t("terminal.page.unknownServer") }}
    </NAlert>
    <NAlert
      v-else-if="mode === 'host' && (!ready || server === null)"
      type="warning"
      :show-icon="true"
      class="term-stage-alert"
    >
      <template #header>{{ t("terminal.page.notReadyTitle") }}</template>
      {{
        t("terminal.page.notReadyBody", {
          status: server ? statusLabel(server) : "",
        })
      }}
    </NAlert>
    <TerminalPane
      v-else-if="tabId !== ''"
      :tab-id="tabId"
      class="term-stage-pane"
      @start-new="emit('start', true)"
    />
    <div
      v-else-if="starting"
      class="term-stage-loading"
      role="status"
      aria-live="polite"
    >
      <NSpin size="small" :aria-label="t('terminal.page.starting')" />
      <p class="small muted">{{ t("terminal.page.starting") }}</p>
    </div>
    <NCard
      v-else
      class="term-stage-closed"
      role="status"
      aria-live="polite"
    >
      <p class="term-closed-title">
        {{ t("terminal.page.sessionClosedTitle") }}
      </p>
      <p class="small muted term-closed-body">
        {{ t("terminal.page.sessionClosedBody") }}
      </p>
      <div class="term-closed-actions">
        <NButton ref="startButton" type="primary" @click="emit('start', true)">
          {{ t("terminal.actions.retry") }}
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
