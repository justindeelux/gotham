import { computed, onUnmounted, ref } from "vue";

import { useAuthStore } from "@/features/auth";
import {
  applyUpdate,
  getUpdateChangelog,
  getUpdateCheck,
  getUpdateSchedule,
  saveUpdateSchedule,
} from "@/features/updates/api/updates";
import type { ScheduleState, UpdateChangelog, UpdateCheck, UpdateSchedule } from "@/features/updates/schemas/updates";

const pollIntervalMs = 3000;
const pollTimeoutMs = 5 * 60 * 1000;
const settledResults = new Set(["ok", "rolled_back", "rollback_failed", "no_backup", "wrapper_failed"]);

function errorText(error: unknown): string {
  const message = (error as { message?: unknown } | null)?.message;
  return typeof message === "string" ? message : "";
}

export function useUpdates() {
  const authStore = useAuthStore();
  const isAdmin = computed(() => authStore.user?.role === "owner");

  const check = ref<UpdateCheck | null>(null);
  const changelog = ref<UpdateChangelog | null>(null);
  const changelogLoading = ref(false);
  const state = ref<ScheduleState | null>(null);
  const checking = ref(false);
  const checkError = ref("");
  const applying = ref(false);
  const applyError = ref("");
  const awaitingRestart = ref(false);
  const saving = ref(false);
  const saveError = ref("");
  const unavailable = ref(false);

  let timer: ReturnType<typeof setTimeout> | null = null;
  onUnmounted(stopPolling);

  function stopPolling(): void {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  async function loadSchedule(): Promise<void> {
    try {
      state.value = await getUpdateSchedule();
    } catch (error) {
      if ((error as { status?: number }).status === 404) {
        unavailable.value = true;
      }
    }
  }

  async function runCheck(): Promise<void> {
    checking.value = true;
    checkError.value = "";
    try {
      check.value = await getUpdateCheck();
    } catch (error) {
      if ((error as { status?: number }).status === 404) {
        unavailable.value = true;
      }
      checkError.value = errorText(error);
    } finally {
      checking.value = false;
      await loadSchedule();
      if (isAdmin.value) {
        await loadChangelog();
      }
    }
  }

  async function load(): Promise<void> {
    await Promise.all([authStore.fetchMe().catch(() => {}), runCheck(), loadSchedule()]);
    if (isAdmin.value) {
      await loadChangelog();
    }
  }

  async function loadChangelog(): Promise<void> {
    changelogLoading.value = true;
    try {
      changelog.value = await getUpdateChangelog();
    } catch {
      changelog.value = null;
    } finally {
      changelogLoading.value = false;
    }
  }

  async function startUpdate(): Promise<void> {
    const before = check.value?.current ?? "";
    applying.value = true;
    applyError.value = "";
    try {
      const result = await applyUpdate();
      if (result.staged) {
        awaitingRestart.value = true;
        poll(before, Date.now());
      } else {
        await runCheck();
      }
    } catch (error) {
      applyError.value = errorText(error);
      await runCheck();
    } finally {
      applying.value = false;
    }
  }

  function poll(before: string, startedAt: number): void {
    timer = setTimeout(async () => {
      try {
        const next = await getUpdateCheck();
        const last = next.last_update;
        if (next.current !== before || (last && settledResults.has(last.result))) {
          check.value = next;
          awaitingRestart.value = false;
          await loadSchedule();
          return;
        }
      } catch {
      }
      if (Date.now() - startedAt > pollTimeoutMs) {
        awaitingRestart.value = false;
        await runCheck();
        return;
      }
      poll(before, startedAt);
    }, pollIntervalMs);
  }

  async function saveSchedule(schedule: UpdateSchedule): Promise<boolean> {
    saving.value = true;
    saveError.value = "";
    try {
      state.value = await saveUpdateSchedule(schedule);
      return true;
    } catch (error) {
      saveError.value = errorText(error);
      return false;
    } finally {
      saving.value = false;
    }
  }

  return {
    isAdmin,
    check,
    changelog,
    changelogLoading,
    loadChangelog,
    state,
    checking,
    checkError,
    applying,
    applyError,
    awaitingRestart,
    saving,
    saveError,
    unavailable,
    load,
    runCheck,
    startUpdate,
    saveSchedule,
  };
}
