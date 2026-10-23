import { computed, onScopeDispose, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useRoute } from "vue-router";

import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";

export interface ApplicationTerminalAccess {
  accessKnown: ComputedRef<boolean>;
  canWrite: ComputedRef<boolean>;
  accessError: ComputedRef<boolean>;
  retryAccess: () => void;
  showTab: ComputedRef<boolean>;
  blocked: ComputedRef<boolean>;
  resolveTab: (_tab: string) => string;
}

export const ACCESS_CHECK_TIMEOUT_MS = 8_000;

export function useApplicationTerminalAccess(
  applicationLoaded: () => boolean,
): ApplicationTerminalAccess {
  const route = useRoute();
  const teamsStore = useTeamsStore();
  const projectsStore = useProjectsStore();
  const timedOut = ref(false);

  const accessKnown = computed<boolean>(
    () => teamsStore.activeTeam !== null || teamsStore.featureDisabled,
  );
  const accessError = computed<boolean>(
    () => !accessKnown.value && (timedOut.value || teamsStore.error !== null),
  );

  let timer: ReturnType<typeof setTimeout> | null = null;
  function scheduleTimeout(): void {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
    if (accessKnown.value || timedOut.value) {
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      timedOut.value = true;
    }, ACCESS_CHECK_TIMEOUT_MS);
  }

  watch(accessKnown, (known) => {
    if (known) {
      timedOut.value = false;
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
      return;
    }
    scheduleTimeout();
  }, { immediate: true });

  onScopeDispose(() => {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  });

  function retryAccess(): void {
    timedOut.value = false;
    scheduleTimeout();
    void teamsStore.fetchTeams().catch(() => undefined);
  }
  const canWrite = computed<boolean>(
    () => accessKnown.value && projectsStore.canWrite,
  );
  const showTab = computed<boolean>(
    () => applicationLoaded() && (canWrite.value || !accessKnown.value),
  );
  const blocked = computed<boolean>(
    () =>
      applicationLoaded() &&
      accessKnown.value &&
      !canWrite.value &&
      route?.query?.tab === "terminal",
  );

  function resolveTab(tab: string): string {
    if (tab === "terminal" && accessKnown.value && !canWrite.value) {
      return "overview";
    }
    return tab;
  }

  return {
    accessKnown,
    accessError,
    retryAccess,
    canWrite,
    showTab,
    blocked,
    resolveTab,
  };
}
