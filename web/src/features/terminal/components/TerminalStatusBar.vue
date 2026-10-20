<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { TerminalTab } from "@/features/terminal/stores/terminal";
import { isLive, statusTone } from "@/features/terminal/utils/terminal";

/** Status footer for one terminal (JUS-249 extraction). */
interface Props {
  tab: TerminalTab;
  variant: "page" | "window";
}

const props = defineProps<Props>();
const { t } = useI18n();

const statusText = computed<string>(() => t(`terminal.status.${props.tab.status}`));
const tone = computed<string>(() => statusTone(props.tab.status));
</script>

<template>
  <footer class="term-statusbar">
    <span class="term-state" :class="`term-state--${tone}`" role="status" aria-live="polite">
      {{ statusText }}
    </span>
    <span v-if="props.tab.shell" class="term-status-item mono">
      {{ props.tab.shell }}
    </span>
    <span class="term-status-item mono">
      {{
        t("terminal.pane.size", {
          cols: props.tab.cols,
          rows: props.tab.rows,
        })
      }}
    </span>
    <span class="grow" />
    <span
      v-if="isLive(props.tab.status) && props.tab.serviceUser"
      class="term-status-item term-user"
      :title="t('terminal.pane.privilege')"
    >
      {{ t("terminal.pane.serviceUser", { user: props.tab.serviceUser }) }}
      <template v-if="props.variant === 'page'">
        · {{ t("terminal.pane.privilege") }}
      </template>
    </span>
  </footer>
</template>

<style scoped>
.term-statusbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-1) var(--space-3);
  border-top: 1px solid var(--border);
  background: var(--surface);
  font-size: var(--text-xs);
  color: var(--muted);
  min-width: 0;
}

.term-state {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  white-space: nowrap;
}

.term-state::before {
  content: "";
  width: var(--space-2);
  height: var(--space-2);
  border-radius: var(--radius-pill);
  background: currentColor;
}

.term-state--success {
  color: var(--success);
}

.term-state--accent {
  color: var(--accent);
}

.term-state--warn {
  color: var(--warn);
}

.term-state--danger {
  color: var(--danger);
}

.term-state--muted {
  color: var(--meta);
}

.term-status-item {
  white-space: nowrap;
}

.term-user {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--meta);
}
</style>
