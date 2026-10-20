import { computed, onScopeDispose, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useRoute } from "vue-router";

import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";

/**
 * Permission gate for the application Terminal tab (JUS-247).
 *
 * The backend needs an owner/admin role in the application's active team, so
 * the UI must not act on `canWrite` while the role is still unknown: the
 * projects store deliberately reads writable until the teams list answers, and
 * issuing a discovery/create in that window would send requests a read_only
 * account may never make. `accessKnown` closes that race; while it is false
 * the tab shows a neutral state and issues zero terminal requests.
 *
 * A control plane with the teams feature disabled has no roles to resolve, so
 * the existing single-tenant write capability is authoritative there.
 */
export interface ApplicationTerminalAccess {
  /** accessKnown is true once the active team (and caller role) resolved. */
  accessKnown: ComputedRef<boolean>;
  /** canWrite mirrors the backend's owner/admin team role. */
  canWrite: ComputedRef<boolean>;
  /** accessError is the bounded failure when the role could not be read. */
  accessError: ComputedRef<boolean>;
  /** retryAccess re-reads the teams list after a bounded failure. */
  retryAccess: () => void;
  /** showTab keeps the tab visible while access is still being checked. */
  showTab: ComputedRef<boolean>;
  /** blocked is the clear not-allowed state for a crafted deep link. */
  blocked: ComputedRef<boolean>;
  /** resolveTab falls back to Overview when Terminal is not allowed. */
  resolveTab: (_tab: string) => string;
}

/**
 * ACCESS_CHECK_TIMEOUT_MS bounds an unanswered teams read. A hanging request
 * must not leave "Checking access" spinning forever; after this the tab shows
 * the same bounded error and explicit Retry as a failed read.
 */
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
  /** scheduleTimeout arms the bounded wait while the role is unknown. */
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

  /** retryAccess clears the bounded failure and re-reads the teams list. */
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

  /** resolveTab keeps a hidden/blocked pane from rendering as Terminal. */
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
