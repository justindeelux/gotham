import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers";
import { useTerminalStore } from "@/features/terminal";

/**
 * Session lifecycle for the server detail Terminal tab (JUS-246).
 *
 * Extracted from ServerDetailPage so the page keeps only tab/URL concerns.
 * The tab is lazy: nothing is created until `active` is true and the node is
 * ready. A close leaves the explicit restart card in place; re-entering the
 * tab never auto-creates (`closed` survives until the operator presses the
 * button). A sign-out is not a demotion: only a still-authenticated account
 * whose operator flag turned false resets the tab and reports `onBlocked`.
 */
export interface ServerTerminalTabOptions {
  serverId: () => string;
  server: () => Server | null;
  ready: () => boolean;
  active: () => boolean;
  onBlocked: () => void;
}

export function useServerTerminalTab(options: ServerTerminalTabOptions) {
  const router = useRouter();
  const authStore = useAuthStore();
  const terminal = useTerminalStore();

  const tabId = ref<string>("");
  const starting = ref(false);
  /** closed marks a tab that vanished under the mounted pane. */
  const closed = ref(false);

  const canUse = computed<boolean>(
    () => authStore.isAuthenticated && authStore.isPlatformAdmin,
  );
  const tab = computed(() =>
    tabId.value === "" ? null : terminal.findTab(tabId.value),
  );

  /** sourcePath is the route the app dock resumes for this session. */
  function sourcePath(id: string): string {
    return router.resolve({
      name: "server-detail",
      params: { id },
      query: { tab: "terminal" },
    }).fullPath;
  }

  /** reset forgets the current pane without touching a live store tab. */
  function reset(): void {
    tabId.value = "";
    starting.value = false;
    closed.value = false;
  }

  /** open creates or reuses the shell; `fresh` is the explicit restart. */
  async function open(fresh: boolean): Promise<void> {
    const server = options.server();
    if (
      !server ||
      server.id !== options.serverId() ||
      !options.ready() ||
      !canUse.value
    ) {
      return;
    }
    const request = {
      serverId: server.id,
      serverName: server.name,
      sourcePath: sourcePath(server.id),
    };
    if (!fresh) {
      const existing = terminal.findTabForTarget(request);
      if (existing) {
        tabId.value = existing.id;
        closed.value = false;
        return;
      }
    }
    closed.value = false;
    starting.value = true;
    try {
      const session = await terminal.openPageSession(
        fresh ? { ...request, fresh: true } : request,
      );
      if (options.serverId() === server.id) {
        tabId.value = session.id;
      }
    } finally {
      starting.value = false;
    }
  }

  /** startNew is the only restart path; it never runs automatically. */
  function startNew(): void {
    void open(true);
  }

  // A tab removed by any route (close confirmation, logout reset) must not
  // leave the host pointing at a dead id.
  watch(tab, (current) => {
    if (tabId.value !== "" && current === null) {
      tabId.value = "";
      starting.value = false;
      closed.value = true;
    }
  });

  // First activation creates; a revisit reuses. `closed` is deliberately NOT
  // cleared here, so switching tabs after a close keeps the restart card and
  // issues zero POSTs until the operator presses it.
  watch(
    [
      () => options.active(),
      () => options.server()?.status,
      () => options.server()?.id,
      canUse,
    ],
    () => {
      if (
        !options.active() ||
        closed.value ||
        tabId.value !== "" ||
        !options.ready() ||
        !canUse.value
      ) {
        return;
      }
      void open(false);
    },
    { immediate: true },
  );

  // Demotion, not sign-out: a logout empties the account but is owned by the
  // auth flow. Only a still-authenticated non-operator resets this tab.
  watch(canUse, (allowed) => {
    if (!allowed && authStore.isAuthenticated) {
      reset();
      options.onBlocked();
    }
  });

  watch(() => options.serverId(), reset);

  return { tabId, starting, closed, startNew };
}
