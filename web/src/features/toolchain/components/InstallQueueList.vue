<script setup lang="ts">
import { Ban, CircleCheck, CircleX, Clock, RotateCcw } from "@lucide/vue";
import { NButton, NEmpty, NIcon, NSpin, NTag, useMessage } from "naive-ui";
import type { Component } from "vue";
import { ref } from "vue";
import { useI18n } from "vue-i18n";

import { getJob } from "@/features/toolchain/api/toolchain";
import {
  describeToolchainError,
} from "@/features/toolchain/api/toolchain";
import type { ToolchainJob } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";
import { isApiError } from "@/features/servers";

const props = defineProps<{ selectedId: string | null }>();

const emit = defineEmits<{ select: [id: string | null] }>();

const { t } = useI18n();
const message = useMessage();
const store = useToolchainStore();

function toolLabel(job: ToolchainJob): string {
  return store.tools.find((tool) => tool.id === job.tool_id)?.name ?? job.tool_id;
}

function actionLabel(job: ToolchainJob): string {
  return job.action === "install"
    ? t("toolchain.actions.install")
    : t("toolchain.actions.uninstall");
}

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

const pending = ref<Record<string, boolean>>({});

function setPending(jobId: string, on: boolean): void {
  const next = { ...pending.value };
  if (on) {
    next[jobId] = true;
  } else {
    delete next[jobId];
  }
  pending.value = next;
}

async function handleCancel(job: ToolchainJob): Promise<void> {
  setPending(job.id, true);
  try {
    await store.cancel(job.id);
  } catch (err) {
    if (isApiError(err) && err.status === 409) {
      try {
        const detail = await getJob(store.serverId, job.id);
        store.applyJob(detail.job);
      } catch {
      }
    }
    message.error(describeToolchainError(err));
  } finally {
    setPending(job.id, false);
  }
}

async function handleRetry(job: ToolchainJob): Promise<void> {
  setPending(job.id, true);
  try {
    const created = await store.retry(job.id);
    if (created.length > 0) {
      emit("select", created[0].id);
      message.success(t("toolchain.modal.retryQueued", { count: created.length }));
      return;
    }
    message.warning(t("toolchain.page.skipped", { tools: job.tool_id }));
    emit("select", store.jobForTool(job.tool_id)?.id ?? null);
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    setPending(job.id, false);
  }
}

</script>

<template>
  <ul v-if="store.jobsList.length > 0" class="queue-list">
    <li
      v-for="job in store.jobsList"
      :key="job.id"
      class="queue-row"
      :class="{ selected: job.id === props.selectedId }"
      :aria-current="job.id === props.selectedId ? 'true' : undefined"
    >
      <button
        type="button"
        class="queue-select"
        :aria-pressed="job.id === props.selectedId"
        :aria-label="toolLabel(job)"
        @click="emit('select', job.id)"
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
      </button>
      <span class="queue-actions">
        <NButton
          v-if="job.status === 'queued'"
          size="tiny"
          ghost
          :loading="pending[job.id] === true"
          @click="() => void handleCancel(job)"
        >
          {{ $t("toolchain.actions.cancel") }}
        </NButton>
        <NButton
          v-else-if="job.status === 'failed'"
          size="tiny"
          ghost
          :loading="pending[job.id] === true"
          @click="() => void handleRetry(job)"
        >
          <template #icon>
            <NIcon><RotateCcw /></NIcon>
          </template>
          {{ $t("toolchain.page.retry") }}
        </NButton>
      </span>
    </li>
  </ul>
  <NEmpty
    v-else
    :description="$t('toolchain.modal.emptyQueue')"
  >
    <template #extra>{{ $t("toolchain.modal.emptyQueueHint") }}</template>
  </NEmpty>
</template>

<style scoped>
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
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg);
}
.queue-row.selected {
  border-color: var(--accent);
}
.queue-select {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 1 1 auto;
  min-width: 0;
  padding: var(--space-1) var(--space-2);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: start;
  cursor: pointer;
}
.queue-select:focus-visible {
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
.queue-actions {
  flex: 0 0 auto;
}
</style>
