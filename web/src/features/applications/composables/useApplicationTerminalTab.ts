import { computed, onScopeDispose, ref, watch } from "vue";
import type { Ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  listApplicationTerminalTargets,
  type ApplicationTerminalTarget,
} from "@/features/applications/api/applications";
import { useAuthStore } from "@/features/auth";
import { applicationTerminalFailure } from "@/features/terminal/api/terminal";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import { useTerminalStore } from "@/features/terminal";
import type { TerminalTab } from "@/features/terminal";
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";

/**
 * Lazy target discovery and session lifecycle for the application Terminal tab
 * (JUS-247).
 *
 * Nothing is requested until the tab is active and the caller holds the
 * application write capability the backend requires. The first activation
 * discovers the running service containers exactly once; a single runnable
 * target opens automatically, while multiple services (or replicas) wait for
 * an explicit selection. Discovery never creates duplicates, a close leaves an
 * explicit restart card, and a service switch always runs the shared close
 * confirmation so a live shell is never killed silently.
 */
export interface ApplicationTerminalTabOptions {
  /** applicationId is the route's application. */
  applicationId: () => string;
  /** applicationName labels the session (fallback: the id). */
  applicationName: () => string;
  /** canWrite mirrors the backend's team write role, never file access. */
  canWrite: () => boolean;
  /** active is true only while the Terminal tab is selected. */
  active: () => boolean;
  /** onBlocked leaves the pane after a demotion/revocation. */
  onBlocked: () => void;
}

/**
 * closedApplications remembers an explicit close across a route remount. The
 * composable's own state dies with the page, but "no auto-create on re-entry"
 * must survive leaving and returning: only "Start new session" removes the
 * marker. It is module state (not persisted, never a credential or transcript).
 */
const closedApplications = new Map<string, string>();

/** resetApplicationTerminalClosedState clears the in-memory restart markers. */
export function resetApplicationTerminalClosedState(): void {
  closedApplications.clear();
}

/** targetKey identifies one discovered container target. */
function targetKey(target: {
  service: string;
  containerId: string;
}): string {
  return `${target.service}\u0000${target.containerId}`;
}

/** tabTargetKey is the same key read back from a live tab. */
function tabTargetKey(tab: TerminalTab): string {
  return `${tab.service}\u0000${tab.containerId}`;
}

export function useApplicationTerminalTab(
  options: ApplicationTerminalTabOptions,
) {
  const route = useRoute();
  const router = useRouter();
  const authStore = useAuthStore();
  const terminal = useTerminalStore();

  const targets: Ref<ApplicationTerminalTarget[]> = ref([]);
  /** serverId is resolved by discovery; container creates never guess it. */
  const serverId = ref("");
  const targetsLoading = ref(false);
  const targetsLoaded = ref(false);
  const targetsError = ref<TerminalErrorCode | null>(null);
  /** attempted makes discovery lazy and never an automatic retry loop. */
  const attempted = ref(false);
  /** selection is the explicit service/container choice (or the live tab). */
  const selection = ref("");
  const tabId = ref("");
  const starting = ref(false);
  /** closed marks a tab that vanished under the mounted pane. */
  const closed = ref(false);

  const canUse = computed<boolean>(
    () => authStore.isAuthenticated && options.canWrite(),
  );
  const tab = computed<TerminalTab | null>(() =>
    tabId.value === "" ? null : terminal.findTab(tabId.value),
  );
  const selected = computed<ApplicationTerminalTarget | null>(
    () =>
      targets.value.find((target) => targetKey({
        service: target.service,
        containerId: target.container_id,
      }) === selection.value) ?? null,
  );
  /** targetOptions is the deterministic, server-ordered visible selection. */
  const targetOptions = computed(() =>
    targets.value.map((target) => ({
      label: target.label || target.name,
      value: targetKey({
        service: target.service,
        containerId: target.container_id,
      }),
    })),
  );
  /** needsSelection is the multi-service/replica state: no session yet. */
  const needsSelection = computed<boolean>(
    () =>
      targetsLoaded.value &&
      tab.value === null &&
      targets.value.length > 1 &&
      selection.value === "",
  );
  const hasTargets = computed<boolean>(() => targets.value.length > 0);
  const mode = computed<"select" | "loading" | "error" | "empty" | "session">(
    () => {
      // A confirmed close keeps the shared stage mounted in its restart state:
      // re-entering the tab or refreshing the list must not auto-create.
      if (closed.value || tab.value !== null || starting.value) {
        return "session";
      }
      if (targetsLoading.value) {
        return "loading";
      }
      if (targetsError.value !== null) {
        return "error";
      }
      if (targetsLoaded.value && targets.value.length === 0) {
        return "empty";
      }
      return "select";
    },
  );

  /** sourcePath is the route the app dock resumes for this page session. */
  function sourcePath(): string {
    return router.resolve({
      name: "application-detail",
      params: {
        projectId: String(route.params.projectId ?? ""),
        environmentId: String(route.params.environmentId ?? ""),
        id: options.applicationId(),
      },
      query: { tab: "terminal" },
    }).fullPath;
  }

  /** resetLocal forgets the pane and discovery state for this application. */
  function resetLocal(): void {
    tabId.value = "";
    starting.value = false;
    closed.value = false;
    selection.value = "";
    targets.value = [];
    targetsLoaded.value = false;
    targetsLoading.value = false;
    targetsError.value = null;
    attempted.value = false;
    serverId.value = "";
  }

  let controller: AbortController | null = null;
  let discoveryToken = 0;

  /** openTarget creates (or reuses) the page session for one exact target. */
  async function openTarget(target: ApplicationTerminalTarget): Promise<void> {
    const applicationId = options.applicationId();
    if (!canUse.value || applicationId === "") {
      return;
    }
    if (tab.value !== null) {
      // A live shell is never replaced without the confirmation flow.
      return;
    }
    closed.value = false;
    closedApplications.delete(applicationId);
    starting.value = true;
    try {
      const opened = await terminal.openPageSession({
        serverId: serverId.value,
        serverName: options.applicationName() || applicationId,
        target: "container",
        applicationId,
        applicationName: options.applicationName(),
        service: target.service,
        containerId: target.container_id,
        containerName: target.name,
        targetLabel: target.label || target.name,
        title: [options.applicationName(), target.label || target.name]
          .filter((part) => part !== "")
          .join(" · "),
        sourcePath: sourcePath(),
      });
      if (options.applicationId() !== applicationId) {
        return;
      }
      tabId.value = opened.id;
      selection.value = targetKey({
        service: target.service,
        containerId: target.container_id,
      });
    } finally {
      starting.value = false;
    }
  }

  /**
   * discover lists the running containers. `autoOpen` is true only for the
   * first activation (or an explicit retry) and still opens only the exact
   * single-target case; a multi-service selector always waits for the user.
   */
  async function discover(autoOpen: boolean): Promise<void> {
    const applicationId = options.applicationId();
    if (applicationId === "" || !canUse.value || targetsLoading.value) {
      return;
    }
    controller?.abort();
    controller = new AbortController();
    const token = ++discoveryToken;
    targetsLoading.value = true;
    targetsError.value = null;
    try {
      const resolved = await listApplicationTerminalTargets(
        applicationId,
        controller.signal,
      );
      if (
        token !== discoveryToken ||
        options.applicationId() !== applicationId
      ) {
        return;
      }
      attempted.value = true;
      targets.value = resolved.containers;
      targetsLoaded.value = true;
      serverId.value = resolved.server_id;
      const live = tab.value;
      if (live !== null) {
        // A refresh never starts a second shell; the live tab stays selected
        // even when the list was reordered or a replica was replaced.
        selection.value = tabTargetKey(live);
        return;
      }
      const stillSelected = targets.value.some(
        (target) =>
          targetKey({
            service: target.service,
            containerId: target.container_id,
          }) === selection.value,
      );
      if (!stillSelected) {
        selection.value = "";
      }
      if (closed.value) {
        return; // the explicit restart card owns the next create
      }
      if (targets.value.length === 1 && autoOpen) {
        await openTarget(targets.value[0]!);
      }
    } catch (error) {
      if (token !== discoveryToken) {
        return;
      }
      attempted.value = true;
      targets.value = [];
      targetsLoaded.value = false;
      targetsError.value = applicationTerminalFailure(error).code;
    } finally {
      if (token === discoveryToken) {
        targetsLoading.value = false;
      }
    }
  }

  /** refresh re-reads the targets without ever creating a duplicate. */
  function refresh(): void {
    void discover(tab.value === null && !closed.value);
  }

  /** retry is the explicit recovery from a bounded discovery error. */
  function retry(): void {
    void discover(tab.value === null && !closed.value);
  }

  /**
   * selectTarget is the explicit service/replica choice. While a shell is
   * live it runs the shared confirmation: Cancel keeps the current session
   * (and restores the selector), Confirm releases exactly that session and
   * opens the chosen target.
   */
  async function selectTarget(key: string): Promise<void> {
    const target =
      targets.value.find(
        (candidate) =>
          targetKey({
            service: candidate.service,
            containerId: candidate.container_id,
          }) === key,
      ) ?? null;
    if (target === null) {
      return;
    }
    const live = tab.value;
    if (live !== null) {
      if (tabTargetKey(live) === key) {
        selection.value = key;
        return;
      }
      const confirmed = await terminal.requestClose([live.id]);
      if (!confirmed) {
        // Cancel keeps the live shell: restore the visible selection to it.
        selection.value = tabTargetKey(live);
        return;
      }
      tabId.value = "";
    }
    selection.value = key;
    closed.value = false;
    await openTarget(target);
  }

  /** startNew is the only restart path after a close; never automatic. */
  async function startNew(): Promise<void> {
    closed.value = false;
    closedApplications.delete(options.applicationId());
    const target = selected.value;
    if (target !== null) {
      await openTarget(target);
      return;
    }
    // The previous target is no longer in this composable's cache (a remount,
    // or it was replaced/stopped): rediscover, then reopen the retained
    // selection explicitly. With no selection the normal selector rule runs.
    attempted.value = false;
    await discover(false);
    const retained =
      selected.value ?? (targets.value.length === 1 ? targets.value[0]! : null);
    if (retained !== null) {
      await openTarget(retained);
    }
  }

  // First activation discovers; a revisit reuses the list. `closed` is
  // deliberately not cleared here, so re-entering the tab keeps the restart
  // card and issues zero creates until the operator presses it.
  watch(
    [
      () => options.active(),
      canUse,
      () => options.applicationId(),
      () => options.applicationName(),
    ],
    () => {
      const applicationId = options.applicationId();
      if (
        !options.active() ||
        !canUse.value ||
        applicationId === "" ||
        closed.value ||
        tabId.value !== "" ||
        attempted.value
      ) {
        return;
      }
      // A remount (route change and back) adopts the live store tab before
      // discovery, so re-entering never opens a second shell for the same
      // container. The list is still refreshed to populate the switch control.
      const existing = terminal.tabs.find(
        (tab) =>
          isContainerTerminalTarget(tab.target) &&
          tab.applicationId === applicationId,
      );
      let autoOpen = true;
      if (existing) {
        tabId.value = existing.id;
        selection.value = tabTargetKey(existing);
        autoOpen = false;
      } else if (closedApplications.has(applicationId)) {
        // A previous explicit close survives the remount: show the shared
        // restart card and create nothing until the operator asks.
        closed.value = true;
        selection.value = closedApplications.get(applicationId) ?? "";
        attempted.value = true;
        return;
      }
      void discover(autoOpen);
    },
    { immediate: true },
  );

  // A tab removed by any route (close confirmation, logout reset) must not
  // leave the host pointing at a dead id. The selection survives so the
  // explicit restart card can offer the same target.
  watch(tab, (current) => {
    if (tabId.value !== "" && current === null) {
      tabId.value = "";
      starting.value = false;
      closed.value = true;
      const applicationId = options.applicationId();
      if (applicationId !== "") {
        closedApplications.set(applicationId, selection.value);
      }
    }
  });

  // Demotion, not sign-out: a logout is owned by the auth flow (which resets
  // the store). A still-authenticated account that lost the write role gets
  // its container tabs disposed and leaves the pane.
  watch(canUse, (allowed) => {
    if (!allowed && authStore.isAuthenticated) {
      terminal.closeContainerSessions(options.applicationId());
      resetLocal();
      options.onBlocked();
    }
  });

  watch(
    () => options.applicationId(),
    () => {
      controller?.abort();
      resetLocal();
    },
  );

  onScopeDispose(() => {
    controller?.abort();
    controller = null;
  });

  return {
    targets,
    targetOptions,
    targetsLoading,
    targetsLoaded,
    targetsError,
    needsSelection,
    hasTargets,
    mode,
    canUse,
    selected,
    selection,
    tab,
    tabId,
    starting,
    closed,
    discover,
    refresh,
    retry,
    selectTarget,
    startNew,
  };
}

export type ApplicationTerminalTabState = ReturnType<
  typeof useApplicationTerminalTab
>;
