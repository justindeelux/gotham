<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { NButton, NSpin } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { TerminalTab } from "@/features/terminal/stores/terminal";
import { errorKey, isRetryable, statusTone } from "@/features/terminal/utils/terminal";

/** Lifecycle overlay for one terminal (JUS-249 extraction). */
interface Props {
  tab: TerminalTab;
}

const props = defineProps<Props>();
const emit = defineEmits<{ retry: [] }>();
const { t } = useI18n();

const tone = computed<string>(() => statusTone(props.tab.status));
const retryable = computed<boolean>(() => isRetryable(props.tab.status));

/** failureText is bounded catalog copy only (JUS-248). Never render raw
 * control-plane or relay text: it can carry host paths. */
const failureText = computed<string>(() =>
  t(errorKey(props.tab.errorCode)),
);

/** exitText describes how the shell ended. */
const exitText = computed<string>(() => {
  if (props.tab.exitSignal) {
    return t("terminal.exited.withSignal", { signal: props.tab.exitSignal });
  }
  if (props.tab.exitCode !== null) {
    return t("terminal.exited.withCode", { code: props.tab.exitCode });
  }
  return t("terminal.exited.ended");
});
</script>

<template>
  <div
    v-if="props.tab.status !== 'connected'"
    class="term-overlay"
    :class="`term-overlay--${tone}`"
  >
    <div v-if="props.tab.status === 'connecting'" class="term-overlay-body">
      <NSpin size="small" :aria-label="t('terminal.status.connecting')" />
      <p class="small muted">{{ t("terminal.status.connecting") }}</p>
    </div>
    <div v-else-if="props.tab.status === 'exited'" class="term-overlay-body">
      <p class="term-overlay-title">{{ t("terminal.exited.title") }}</p>
      <p class="small muted">{{ exitText }}</p>
      <NButton size="small" secondary @click.stop="emit('retry')">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("terminal.actions.retry") }}
      </NButton>
    </div>
    <div v-else-if="props.tab.status === 'disconnected'" class="term-overlay-body">
      <p class="term-overlay-title">{{ t("terminal.disconnected.title") }}</p>
      <p class="small muted">{{ t("terminal.disconnected.body") }}</p>
      <NButton size="small" secondary @click.stop="emit('retry')">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("terminal.actions.retry") }}
      </NButton>
    </div>
    <div v-else-if="props.tab.status === 'error'" class="term-overlay-body">
      <p class="term-overlay-title">{{ t("terminal.errors.title") }}</p>
      <p class="small muted">{{ failureText }}</p>
      <NButton v-if="retryable" size="small" secondary @click.stop="emit('retry')">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("terminal.actions.retry") }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.term-overlay {
  position: absolute;
  /* Above the suggestion overlay so a reconnect state is never covered. */
  z-index: 4;
  inset: auto var(--space-3) calc(var(--space-8) + var(--space-2));
  display: grid;
  place-items: center;
  pointer-events: none;
}

.term-overlay-body {
  pointer-events: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  max-width: min(26rem, 100%);
  padding: var(--space-4);
  text-align: center;
  background: color-mix(in oklab, var(--surface) 92%, transparent);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-raised);
}

.term-overlay-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.term-detail {
  max-width: 100%;
  overflow-wrap: anywhere;
  color: var(--meta);
}
</style>
