<script setup lang="ts">
import { Terminal } from "@lucide/vue";
import { NButton, NIcon, NSpin } from "naive-ui";
import { nextTick, ref, watch } from "vue";

import type { QueuedLogLine } from "@/features/toolchain/composables/useInstallQueueLog";

/** Render state of the log pane, decided by the modal from job + tail state. */
export type InstallLogPaneState =
  | "idle"
  | "loading"
  | "error"
  | "empty-waiting"
  | "empty"
  | "ready";

const props = defineProps<{
  title: string;
  state: InstallLogPaneState;
  lines: QueuedLogLine[];
}>();

const emit = defineEmits<{ reload: [] }>();

// Follow-tail state: appended lines auto-scroll until the reader scrolls up
// (48px off the tail), and a jump button offers the way back down. The
// watcher keys on the last line's seq, not the buffer length, so rotation
// under the client-side cap keeps following.
const logBody = ref<HTMLElement | null>(null);
const following = ref(true);

/** lastSeq is the tail watermark; length would freeze once the cap fills. */
const lastSeq = (): number | null => {
  const tail = props.lines[props.lines.length - 1];
  return tail ? tail.seq : null;
};

/** handleLogScroll leaves follow mode once the reader scrolls off the tail. */
function handleLogScroll(): void {
  const el = logBody.value;
  if (!el) {
    return;
  }
  following.value = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
}

/** jumpToLatest resumes the tail and scrolls to it. */
function jumpToLatest(): void {
  following.value = true;
  const el = logBody.value;
  if (el) {
    el.scrollTop = el.scrollHeight;
  }
}

/**
 * toggleFollow pauses the tail (the reader scrolled up to inspect history)
 * or resumes it. The label already flips (Follow/Following), so no
 * aria-pressed: a screen reader would otherwise hear "Following, pressed".
 */
function toggleFollow(): void {
  if (following.value) {
    following.value = false;
    return;
  }
  jumpToLatest();
}

watch(lastSeq, () => {
  if (following.value) {
    void nextTick(jumpToLatest);
  }
});
</script>

<template>
  <section class="log-section" :aria-label="$t('toolchain.modal.logLabel')">
    <div class="log-head">
      <NIcon :size="16" aria-hidden="true"><Terminal /></NIcon>
      <strong>{{ props.title }}</strong>
      <span class="log-head-actions">
        <NButton
          size="tiny"
          secondary
          :disabled="props.state === 'idle' || props.state === 'loading'"
          @click="toggleFollow"
        >
          {{ following ? $t("toolchain.modal.following") : $t("toolchain.modal.follow") }}
        </NButton>
        <NButton
          size="tiny"
          secondary
          :disabled="props.state === 'idle' || props.state === 'loading'"
          @click="emit('reload')"
        >
          {{ $t("toolchain.modal.refreshLog") }}
        </NButton>
      </span>
    </div>

    <!-- aria-live is off on the stream itself: a 500-line tail is too chatty
         for a live region, and the modal's status region announces state
         changes instead. The pane stays keyboard-scrollable. -->
    <div
      ref="logBody"
      class="log-pane"
      aria-live="off"
      :aria-label="$t('toolchain.modal.logLabel')"
      tabindex="0"
      @scroll="handleLogScroll"
    >
      <p v-if="props.state === 'idle'" class="muted log-hint">
        {{ $t("toolchain.modal.selectJobHint") }}
      </p>
      <p v-else-if="props.state === 'loading'" class="muted log-hint">
        <NSpin :size="14" /> {{ $t("toolchain.modal.loadingLog") }}
      </p>
      <div v-else-if="props.state === 'error'" class="log-error">
        <p>{{ $t("toolchain.modal.logError") }}</p>
        <NButton size="small" ghost @click="emit('reload')">
          {{ $t("toolchain.page.retry") }}
        </NButton>
      </div>
      <p v-else-if="props.state === 'empty-waiting'" class="muted log-hint">
        {{ $t("toolchain.modal.logWaiting") }}
      </p>
      <p v-else-if="props.state === 'empty'" class="muted log-hint">
        {{ $t("toolchain.modal.logEmpty") }}
      </p>
      <!-- Plain text interpolation only: log content is untrusted agent
           output and must never render as HTML. -->
      <template v-else>
        <div
          v-for="line in props.lines"
          :key="line.seq"
          class="log-line"
        >{{ line.text }}</div>
      </template>
    </div>

    <NButton
      v-if="!following && props.lines.length > 0"
      size="small"
      secondary
      class="jump-latest"
      @click="jumpToLatest"
    >
      {{ $t("toolchain.modal.jumpToLatest") }}
    </NButton>
  </section>
</template>

<style scoped>
.log-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  position: relative;
}
.log-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--fg);
}
.log-head-actions {
  margin-left: auto;
  display: flex;
  gap: var(--space-2);
}
.log-pane {
  height: 300px;
  overflow-y: auto;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-2) var(--space-3);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  line-height: 1.6;
  color: var(--fg);
}
.log-line {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.log-line:empty::before {
  content: "\00a0";
}
.log-hint {
  margin: 0;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.log-error {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  align-items: flex-start;
}
.log-error p {
  margin: 0;
  color: var(--fg);
}
.jump-latest {
  position: absolute;
  right: var(--space-3);
  bottom: var(--space-3);
}
</style>
