<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { isTerminalJobStatus } from "@/features/toolchain/api/toolchain";
import type { ToolchainJob } from "@/features/toolchain/api/toolchain";
import InstallJobLogPane from "@/features/toolchain/components/InstallJobLogPane.vue";
import type { InstallLogPaneState } from "@/features/toolchain/components/InstallJobLogPane.vue";
import InstallQueueList from "@/features/toolchain/components/InstallQueueList.vue";
import { useInstallQueueLog } from "@/features/toolchain/composables/useInstallQueueLog";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const { t } = useI18n();
const store = useToolchainStore();

const show = computed<boolean>({
  get: () => store.queueModalOpen,
  set: (value: boolean) => {
    if (!value) {
      store.closeQueueModal();
    }
  },
});

const selectedJobId = ref<string | null>(null);

const manualJobId = ref<string | null>(null);

const lastWanted = ref<string | null | undefined>(undefined);

const selectedJob = computed<ToolchainJob | null>(
  () => store.jobs[selectedJobId.value ?? ""] ?? null,
);

const logActive = computed<boolean>(
  () => selectedJob.value !== null && !isTerminalJobStatus(selectedJob.value.status),
);

const log = useInstallQueueLog({
  serverId: computed(() => store.serverId),
  jobId: selectedJobId,
  enabled: show,
  active: logActive,
});

const voiceStatus = computed<string>(() => {
  if (selectedJob.value === null) {
    return "";
  }
  const tool =
    store.tools.find((entry) => entry.id === selectedJob.value?.tool_id)?.name ??
    selectedJob.value.tool_id;
  const action =
    selectedJob.value.action === "install"
      ? t("toolchain.actions.install")
      : t("toolchain.actions.uninstall");
  return t("toolchain.modal.jobStatus", {
    job: t("toolchain.modal.selectedJob", { tool, action }),
    status: t(`toolchain.queue.${selectedJob.value.status}`),
  });
});

const paneTitle = computed<string>(() => {
  if (selectedJob.value === null) {
    return t("toolchain.modal.logLabel");
  }
  const tool =
    store.tools.find((entry) => entry.id === selectedJob.value?.tool_id)?.name ??
    selectedJob.value.tool_id;
  const action =
    selectedJob.value.action === "install"
      ? t("toolchain.actions.install")
      : t("toolchain.actions.uninstall");
  return t("toolchain.modal.selectedJob", { tool, action });
});

const paneState = computed<InstallLogPaneState>(() => {
  if (selectedJob.value === null) {
    return "idle";
  }
  if (log.loading.value) {
    return "loading";
  }
  if (log.failure.value !== null) {
    return "error";
  }
  if (log.lines.value.length === 0) {
    return logActive.value ? "empty-waiting" : "empty";
  }
  return "ready";
});

function pickFollowedId(jobs: ToolchainJob[]): string | null {
  return (
    jobs.find((job) => job.status === "running")?.id ??
    jobs.find((job) => job.status === "queued")?.id ??
    jobs[0]?.id ??
    null
  );
}

function onSelect(id: string | null): void {
  selectedJobId.value = id;
  manualJobId.value = id;
}

watch(
  show,
  (open) => {
    if (!open) {
      selectedJobId.value = null;
      manualJobId.value = null;
      lastWanted.value = undefined;
      return;
    }
    lastWanted.value = store.queueModalJobId;
    const jobs = store.jobsList;
    const wanted = store.queueModalJobId;
    const pinned = wanted !== null && jobs.some((job) => job.id === wanted) ? wanted : null;
    manualJobId.value = pinned;
    selectedJobId.value = pinned ?? pickFollowedId(jobs);
  },
  { immediate: true },
);

watch(
  [() => store.jobsList, () => store.queueModalJobId],
  ([jobs, wanted]) => {
    if (!show.value) {
      return;
    }
    if (wanted !== lastWanted.value) {
      lastWanted.value = wanted;
      if (wanted !== null && jobs.some((job) => job.id === wanted)) {
        manualJobId.value = wanted;
        selectedJobId.value = wanted;
        return;
      }
    }
    if (manualJobId.value !== null) {
      if (jobs.some((job) => job.id === manualJobId.value)) {
        selectedJobId.value = manualJobId.value;
        return;
      }
      manualJobId.value = null;
    }
    selectedJobId.value = pickFollowedId(jobs);
  },
);
</script>

<template>
  <NModal
    v-model:show="show"
    preset="card"
    :title="$t('toolchain.modal.title')"
    :trap-focus="true"
    class="app-modal install-queue-modal"
    style="width: 880px; max-width: 96vw"
  >
    <div class="queue-modal-body">
      <section :aria-label="$t('toolchain.modal.queueLabel')">
        <InstallQueueList
          :selected-id="selectedJobId"
          @select="onSelect"
        />
        <p class="modal-sr" aria-live="polite">{{ voiceStatus }}</p>
      </section>

      <InstallJobLogPane
        :title="paneTitle"
        :state="paneState"
        :lines="log.lines.value"
        @reload="() => void log.reload()"
      />
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
.queue-modal-foot {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}
.close-hint {
  font-size: var(--text-xs);
  margin-right: auto;
}
.modal-sr {
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
