<script setup lang="ts">
import { Ban, CircleCheck, CircleX, Clock, RotateCcw, Terminal } from "@lucide/vue";
import {
  NButton,
  NEmpty,
  NIcon,
  NModal,
  NSpin,
  NTag,
  useMessage,
} from "naive-ui";
import type { Component } from "vue";
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeToolchainError,
  isTerminalJobStatus,
} from "@/features/toolchain/api/toolchain";
import type { ToolchainJob } from "@/features/toolchain/api/toolchain";
import { useInstallQueueLog } from "@/features/toolchain/composables/useInstallQueueLog";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const { t } = useI18n();
const message = useMessage();
const store = useToolchainStore();

/**
 * show mirrors the JUS-178 hook flag: any screen flips it via
 * `openQueueModal`, and closing here only hides the modal — the queue keeps
 * running in the background (footer copy says so).
 */
const show = computed<boolean>({
  get: () => store.queueModalOpen,
  set: (value: boolean) => {
    if (!value) {
      store.closeQueueModal();
    }
  },
});

/** selectedJobId names the job whose log renders; null shows the hint. */
const selectedJobId = ref<string | null>(null);

/** selectedJob resolves the selection against the live queue. */
const selectedJob = computed<ToolchainJob | null>(
  () => store.jobs[selectedJobId.value ?? ""] ?? null,
);

/** logActive gates the cursor catch-up poll while the selection runs. */
const logActive = computed<boolean>(
  () => selectedJob.value !== null && !isTerminalJobStatus(selectedJob.value.status),
);

const log = useInstallQueueLog({
  serverId: computed(() => store.serverId),
  jobId: selectedJobId,
  enabled: show,
  active: logActive,
});

/** toolLabel renders the catalog name, falling back to the raw tool id. */
function toolLabel(job: ToolchainJob): string {
  return store.tools.find((tool) => tool.id === job.tool_id)?.name ?? job.tool_id;
}

/** actionLabel renders install/uninstall in the display locale. */
function actionLabel(job: ToolchainJob): string {
  return job.action === "install"
    ? t("toolchain.actions.install")
    : t("toolchain.actions.uninstall");
}

/** tagType maps a job status to one Naive tag (mirrors the page badges). */
function tagType(status: ToolchainJob["status"]): "default" | "info" | "success" | "error" {
  switch (status) {
    case "running":
      return "info";
    case "succeeded":
      return "success";
    case "failed":
      return "error";
    default:
      return "default";
  }
}

/** statusIcon names the per-status icon; running renders a spinner instead. */
function statusIcon(status: ToolchainJob["status"]): Component | null {
  switch (status) {
    case "queued":
      return Clock;
    case "succeeded":
      return CircleCheck;
    case "failed":
      return CircleX;
    case "canceled":
      return Ban;
    default:
      return null;
  }
}

/**
 * voiceStatus announces the selection in a polite live region (the queue
 * list itself is too chatty to be live: every realtime frame re-renders it).
 */
const voiceStatus = computed<string>(() =>
  selectedJob.value === null
    ? ""
    : `${toolLabel(selectedJob.value)} ${actionLabel(selectedJob.value)}: ${t(`toolchain.queue.${selectedJob.value.status}`)}`,
);

/** pending tracks per-job cancel/retry spinners by job id. */
const pending = ref<Record<string, boolean>>({});

/** handleCancel cancels a queued job (running/terminal answers 409). */
async function handleCancel(job: ToolchainJob): Promise<void> {
  pending.value = { ...pending.value, [job.id]: true };
  try {
    await store.cancel(job.id);
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    const next = { ...pending.value };
    delete next[job.id];
    pending.value = next;
  }
}

/**
 * handleRetry re-enqueues a failed job's tool action as a fresh job and
 * focuses the new attempt so its output starts streaming immediately.
 */
async function handleRetry(job: ToolchainJob): Promise<void> {
  pending.value = { ...pending.value, [job.id]: true };
  try {
    const created = await store.retry(job.id);
    if (created.length > 0) {
      selectedJobId.value = created[0].id;
      message.success(t("toolchain.modal.retryQueued", { count: created.length }));
    }
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    const next = { ...pending.value };
    delete next[job.id];
    pending.value = next;
  }
}

// Follow-tail state for the log pane: appended lines auto-scroll until the
// reader scrolls up, and a jump button offers the way back down.
const logBody = ref<HTMLElement | null>(null);
const following = ref(true);

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

watch(
  () => log.lines.value.length,
  () => {
    if (following.value) {
      void nextTick(jumpToLatest);
    }
  },
);

// Opening (re)focuses the requested job, the first active job, or the
// newest job; a selection that outlives its job (or an empty queue that
// gains jobs) falls back the same way.
watch(
  [show, () => store.jobsList],
  ([open, jobs]) => {
    if (!open) {
      return;
    }
    const wanted = selectedJobId.value !== null ? jobs.find((job) => job.id === selectedJobId.value) : undefined;
    if (wanted) {
      return;
    }
    selectedJobId.value =
      (store.queueModalJobId !== null && jobs.some((job) => job.id === store.queueModalJobId)
        ? store.queueModalJobId
        : undefined) ??
      store.activeJobs[0]?.id ??
      jobs[0]?.id ??
      null;
    following.value = true;
  },
  { immediate: true },
);
</script>

<template>
  <NModal
    v-model:show="show"
    preset="card"
    :title="t('toolchain.modal.title')"
    :trap-focus="true"
    class="app-modal install-queue-modal"
    style="width: 880px; max-width: 96vw"
  >
    <div class="queue-modal-body">
      <section :aria-label="t('toolchain.modal.queueLabel')">
        <ul v-if="store.jobsList.length > 0" class="queue-list" role="listbox">
          <li
            v-for="job in store.jobsList"
            :key="job.id"
            role="option"
            :aria-selected="job.id === selectedJobId"
            tabindex="0"
            class="queue-row"
            :class="{ selected: job.id === selectedJobId }"
            @click="selectedJobId = job.id"
            @keydown.enter="selectedJobId = job.id"
            @keydown.space.prevent="selectedJobId = job.id"
          >
            <span class="queue-status" aria-hidden="true">
              <NSpin v-if="job.status === 'running'" :size="16" />
              <NIcon v-else-if="statusIcon(job.status)" :size="16">
                <component :is="statusIcon(job.status)" />
              </NIcon>
            </span>
            <span class="queue-main">
              <span class="queue-tool">{{ toolLabel(job) }}</span>
              <span class="muted queue-meta">{{ actionLabel(job) }}</span>
            </span>
            <NTag :type="tagType(job.status)" size="small" round>
              {{ $t(`toolchain.queue.${job.status}`) }}
            </NTag>
            <NButton
              v-if="job.status === 'queued'"
              size="tiny"
              ghost
              :loading="pending[job.id] === true"
              @click.stop="() => void handleCancel(job)"
            >
              {{ $t("toolchain.actions.cancel") }}
            </NButton>
            <NButton
              v-else-if="job.status === 'failed'"
              size="tiny"
              ghost
              :loading="pending[job.id] === true"
              @click.stop="() => void handleRetry(job)"
            >
              <template #icon>
                <NIcon><RotateCcw /></NIcon>
              </template>
              {{ $t("toolchain.page.retry") }}
            </NButton>
          </li>
        </ul>
        <NEmpty
          v-else
          :description="t('toolchain.modal.emptyQueue')"
        >
          <template #extra>{{ $t("toolchain.modal.emptyQueueHint") }}</template>
        </NEmpty>
        <p class="sr-only" aria-live="polite">{{ voiceStatus }}</p>
      </section>

      <section class="log-section" :aria-label="t('toolchain.modal.logLabel')">
        <div class="log-head">
          <NIcon :size="16" aria-hidden="true"><Terminal /></NIcon>
          <strong v-if="selectedJob">{{ t("toolchain.modal.selectedJob", { tool: toolLabel(selectedJob), action: actionLabel(selectedJob) }) }}</strong>
          <strong v-else>{{ $t("toolchain.modal.logLabel") }}</strong>
          <span class="log-head-actions">
            <NButton
              size="tiny"
              secondary
              :disabled="selectedJob === null || log.loading.value"
              @click="toggleFollow"
            >
              {{ following ? $t("toolchain.modal.following") : $t("toolchain.modal.follow") }}
            </NButton>
            <NButton
              size="tiny"
              secondary
              :disabled="selectedJob === null || log.loading.value"
              @click="() => void log.reload()"
            >
              {{ $t("toolchain.modal.refreshLog") }}
            </NButton>
          </span>
        </div>

        <div
          ref="logBody"
          class="log-pane"
          role="log"
          aria-live="polite"
          aria-relevant="additions"
          :aria-label="t('toolchain.modal.logLabel')"
          tabindex="0"
          @scroll="handleLogScroll"
        >
          <p v-if="selectedJob === null" class="muted log-hint">
            {{ $t("toolchain.modal.selectJobHint") }}
          </p>
          <p v-else-if="log.loading.value" class="muted log-hint">
            <NSpin :size="14" /> {{ $t("toolchain.modal.loadingLog") }}
          </p>
          <div v-else-if="log.failure.value !== null" class="log-error">
            <p>{{ $t("toolchain.modal.logError") }}</p>
            <NButton size="small" ghost @click="() => void log.reload()">
              {{ $t("toolchain.page.retry") }}
            </NButton>
          </div>
          <p v-else-if="log.lines.value.length === 0" class="muted log-hint">
            {{ logActive ? $t("toolchain.modal.logWaiting") : $t("toolchain.modal.logEmpty") }}
          </p>
          <!-- Plain text interpolation only: log content is untrusted agent
               output and must never render as HTML. -->
          <div
            v-for="line in log.lines.value"
            :key="line.seq"
            class="log-line"
          >{{ line.text }}</div>
        </div>

        <NButton
          v-if="!following && log.lines.value.length > 0"
          size="small"
          secondary
          class="jump-latest"
          @click="jumpToLatest"
        >
          {{ $t("toolchain.modal.jumpToLatest") }}
        </NButton>
      </section>
    </div>

    <template #footer>
      <div class="queue-modal-foot">
        <span class="muted close-hint">{{ $t("toolchain.modal.closeHint") }}</span>
        <NButton @click="store.closeQueueModal()">{{ $t("toolchain.modal.close") }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.queue-modal-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.queue-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  max-height: 220px;
  overflow-y: auto;
}
.queue-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  cursor: pointer;
  background: var(--bg);
}
.queue-row.selected {
  border-color: var(--accent);
}
.queue-row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.queue-status {
  display: inline-flex;
  align-items: center;
  color: var(--muted);
  flex: 0 0 auto;
}
.queue-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 auto;
  min-width: 0;
}
.queue-tool {
  color: var(--fg);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.queue-meta {
  font-size: var(--text-xs);
}
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
.queue-modal-foot {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}
.close-hint {
  font-size: var(--text-xs);
  margin-right: auto;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}
</style>
